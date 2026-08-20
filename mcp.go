// mcp.go — gspark como servidor MCP por stdio. Expone `retrieve` (notas rankeadas) y
// `reindex` (refresca el índice). Read-only sobre el vault, local, sin red (el cliente lo lanza como
// subproceso). Auto-reindex por staleness antes de cada retrieve — si alguna nota .md es más nueva
// que el índice, reconstruye. Barato (WalkDir de mtimes), sin daemon.
package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// indiceVivo: índice + motor + marca de tiempo, protegido para refresco concurrente-seguro.
type indiceVivo struct {
	mu       sync.RWMutex
	vault    string
	engName  string
	idx      *Indice
	eng      Engine
	indexado time.Time // cuándo se construyó el índice actual
}

// mtimeMax: el mtime más reciente de cualquier .md del vault (barato; salta ocultos/_lab/node_modules).
func mtimeMax(raiz string) time.Time {
	var max time.Time
	filepath.WalkDir(raiz, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			n := d.Name()
			if (n != "." && strings.HasPrefix(n, ".")) || n == "_lab" || n == "node_modules" {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(strings.ToLower(d.Name()), ".md") {
			if fi, e := d.Info(); e == nil && fi.ModTime().After(max) {
				max = fi.ModTime()
			}
		}
		return nil
	})
	return max
}

// reconstruir arma índice+motor de cero. Devuelve nº de notas o error.
func (v *indiceVivo) reconstruir() (int, error) {
	idx, err := construirIndice(v.vault)
	if err != nil {
		return 0, err
	}
	v.mu.Lock()
	v.idx = idx
	v.eng = nuevoEngine(v.engName, idx)
	v.indexado = time.Now()
	v.mu.Unlock()
	return idx.N, nil
}

// frescar: reconstruye SOLO si el vault cambió desde el último indexado (staleness-based). Devuelve si refrescó.
func (v *indiceVivo) frescar() bool {
	v.mu.RLock()
	stamp := v.indexado
	v.mu.RUnlock()
	if mtimeMax(v.vault).After(stamp) {
		if _, err := v.reconstruir(); err == nil {
			fmt.Fprintf(os.Stderr, "[gspark-mcp] auto-reindex: el vault cambió → índice refrescado\n")
			return true
		}
	}
	return false
}

func runMCP(args []string) int {
	vault := flag(args, "--vault", os.Getenv("GSPARK_VAULT"))
	if vault == "" {
		fmt.Fprintln(os.Stderr, "uso: gspark mcp --vault <ruta>  (o GSPARK_VAULT=<ruta>)")
		return 2
	}
	v := &indiceVivo{vault: vault, engName: flag(args, "--engine", "bm25")}
	n, err := v.reconstruir()
	if err != nil {
		fmt.Fprintln(os.Stderr, "mcp: no pude indexar el vault:", err)
		return 1
	}
	fmt.Fprintf(os.Stderr, "[gspark-mcp] listo: %d notas · motor %s · vault %s (auto-reindex por staleness ON)\n", n, v.engName, vault)

	s := server.NewMCPServer("gspark", "0.2.0")

	s.AddTool(mcp.NewTool("retrieve",
		mcp.WithDescription("Busca en el vault de conocimiento las notas más relevantes para una consulta "+
			"y devuelve sus rutas + puntaje. Úsalo ANTES de trabajar en algo (\"voy a hacer X\") para ver si "+
			"ya existe una nota, decisión o war-story relevante que debas leer primero. El índice se refresca "+
			"solo si el vault cambió (ves notas recién escritas)."),
		mcp.WithString("query", mcp.Required(), mcp.Description("Lo que estás por hacer o buscar, en lenguaje natural.")),
		mcp.WithNumber("top", mcp.Description("Cuántos resultados devolver (por defecto 5).")),
	), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		q := strings.TrimSpace(req.GetString("query", ""))
		if q == "" {
			return mcp.NewToolResultText("Error: la consulta viene vacía."), nil
		}
		top := req.GetInt("top", 5)
		if top < 1 {
			top = 5
		}
		v.frescar() // FRESCURA: refresca si el vault cambió, antes de buscar
		v.mu.RLock()
		eng, vault := v.eng, v.vault
		res := eng.Buscar(q)
		v.mu.RUnlock()
		if len(res) == 0 {
			return mcp.NewToolResultText("Sin coincidencias en el vault para: " + q), nil
		}
		n := min(top, len(res))
		var b strings.Builder
		fmt.Fprintf(&b, "%d nota(s) relevante(s) para %q (motor %s):\n", n, q, eng.Nombre())
		for i := 0; i < n; i++ {
			fmt.Fprintf(&b, "%d. %s  ·  score %d\n", i+1, rutaRelativa(res[i].Ruta, vault), res[i].Puntaje)
		}
		return mcp.NewToolResultText(b.String()), nil
	})

	s.AddTool(mcp.NewTool("reindex",
		mcp.WithDescription("Reconstruye el índice del vault desde cero (fuerza la absorción de notas nuevas/"+
			"cambiadas/borradas). Normalmente no hace falta llamarla — retrieve ya auto-refresca por cambios; "+
			"úsala si quieres forzar el refresco explícitamente."),
	), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		n, err := v.reconstruir()
		if err != nil {
			return mcp.NewToolResultText("Error al reindexar: " + err.Error()), nil
		}
		return mcp.NewToolResultText(fmt.Sprintf("Índice reconstruido: %d notas.", n)), nil
	})

	if err := server.ServeStdio(s); err != nil {
		fmt.Fprintln(os.Stderr, "mcp: servidor terminó con error:", err)
		return 1
	}
	return 0
}
