// Command gspark audits the health of a Markdown knowledge vault.
//
//   - `gspark eval`   runs the retrieval exam (query -> expected note) and scores how well the
//     vault answers, with a swappable engine (--engine keyword|bm25). The exam
//     (queries.json) never changes, so "keyword vs bm25" is directly comparable.
//   - `gspark search` answers a query: for operational-class queries (IP/host/credential) it
//     returns a POINTER to where the data lives for the active entity,
//     never the data itself; otherwise it returns the top notes from the engine.
//
// Read-only, single portable Go binary, zero external services (cross-compiles to Windows/WSL2).
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Query is one exam item: a human-phrased question and a distinctive substring of the
// path of the canonical note that SHOULD answer it.
// Query: una pregunta del examen. Schema válido: match por IDENTIDAD de nota (ruta relativa exacta).
// Mantiene compat con el schema legacy (pregunta/esperada por substring) para no romper exámenes viejos.
type Query struct {
	ID string `json:"id"`
	// Schema válido (examen nuevo):
	Query      string   `json:"query,omitempty"`      // pregunta redactada como humano
	Target     string   `json:"target,omitempty"`     // ruta RELATIVA al vault (identidad canónica; DEBE existir)
	Targets    []string `json:"targets,omitempty"`    // varias notas válidas (alternativa a target; acierto si cualquiera aparece)
	Dificultad string   `json:"dificultad,omitempty"` // lexico | parafrasis | semantico
	Categoria  string   `json:"categoria,omitempty"`
	// Legacy (examen viejo, match por substring en el path):
	Pregunta string `json:"pregunta,omitempty"`
	Esperada string `json:"esperada,omitempty"`
}

// texto: el enunciado de la consulta (schema nuevo o legacy).
func (q Query) texto() string {
	if q.Query != "" {
		return q.Query
	}
	return q.Pregunta
}

// objetivos: rutas-target aceptables (schema nuevo). Vacío ⇒ usar el fallback legacy por substring.
func (q Query) objetivos() []string {
	if len(q.Targets) > 0 {
		return q.Targets
	}
	if q.Target != "" {
		return []string{q.Target}
	}
	return nil
}

// rutaRelativa: path de un resultado relativo a la raíz del vault (para comparar contra target).
func rutaRelativa(ruta, vault string) string {
	if rel, err := filepath.Rel(vault, ruta); err == nil {
		return rel
	}
	return ruta
}

// Resultado is a scored note from a retrieval run (shared with retrieval.go).
type Resultado struct {
	Ruta    string
	Puntaje int
}

// Veredicto is the outcome of grading one query.
type Veredicto struct {
	ID       string   `json:"id"`
	Pregunta string   `json:"pregunta"`
	Esperada string   `json:"esperada"`
	Estado   string   `json:"estado"` // "ok" | "parcial" | "fallo"
	Posicion int      `json:"posicion"`
	Top      []string `json:"top"`
}

// Reporte is the full machine-readable output.
type Reporte struct {
	Vault        string      `json:"vault"`
	Engine       string      `json:"engine"`
	Notas        int         `json:"notas_indexadas"`
	Total        int         `json:"preguntas"`
	OK           int         `json:"ok"`
	Parcial      int         `json:"parcial"`
	Fallo        int         `json:"fallo"`
	EstrictoPct  float64     `json:"estricto_pct"`
	PonderadoPct float64     `json:"ponderado_pct"`
	TopN         int         `json:"top_n"`
	Veredictos   []Veredicto `json:"veredictos"`
}

// evaluar grades one query. Schema válido: acierto = la IDENTIDAD del target (ruta relativa exacta)
// aparece entre los resultados; ok si en top-N, parcial si más abajo, fallo si nunca. Legacy: si la
// query no trae target, cae al match por substring de Esperada en el path (examen viejo).
func evaluar(q Query, resultados []Resultado, topN int, vault string) Veredicto {
	objetivos := q.objetivos()
	etiqueta := q.Esperada
	if len(objetivos) > 0 {
		etiqueta = strings.Join(objetivos, " | ")
	}
	v := Veredicto{ID: q.ID, Pregunta: q.texto(), Esperada: etiqueta, Estado: "fallo", Posicion: -1}
	esperada := strings.ToLower(q.Esperada)
	for i, r := range resultados {
		if i < topN {
			v.Top = append(v.Top, filepath.Base(r.Ruta))
		}
		if v.Posicion != -1 {
			continue
		}
		acierto := false
		if len(objetivos) > 0 {
			rel := rutaRelativa(r.Ruta, vault)
			for _, t := range objetivos {
				if rel == t {
					acierto = true
					break
				}
			}
		} else if esperada != "" && strings.Contains(strings.ToLower(r.Ruta), esperada) {
			acierto = true // fallback legacy por substring
		}
		if acierto {
			v.Posicion = i + 1
		}
	}
	switch {
	case v.Posicion >= 1 && v.Posicion <= topN:
		v.Estado = "ok"
	case v.Posicion > topN:
		v.Estado = "parcial"
	}
	return v
}

// flag reads --name value from args (simple, no external deps).
func flag(args []string, nombre, def string) string {
	for i := 0; i < len(args)-1; i++ {
		if args[i] == nombre {
			return args[i+1]
		}
	}
	return def
}

func runEval(args []string) int {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "uso: gspark eval <vault> [--engine keyword|bm25] [--queries eval/queries.json] [--report report.json] [--top 3]")
		return 2
	}
	vault := args[0]
	queriesPath := flag(args, "--queries", "eval/queries.json")
	reportPath := flag(args, "--report", "report.json")
	engineName := flag(args, "--engine", "bm25")
	topN := 3
	if t := flag(args, "--top", "3"); t != "3" {
		fmt.Sscanf(t, "%d", &topN)
	}

	datosQ, err := os.ReadFile(queriesPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "no pude leer el examen:", err)
		return 1
	}
	var queries []Query
	if err := json.Unmarshal(datosQ, &queries); err != nil {
		fmt.Fprintln(os.Stderr, "el examen no es JSON válido:", err)
		return 1
	}

	idx, err := construirIndice(vault)
	if err != nil {
		fmt.Fprintln(os.Stderr, "no pude indexar el vault:", err)
		return 1
	}
	eng := nuevoEngine(engineName, idx)

	rep := Reporte{Vault: vault, Engine: eng.Nombre(), Notas: idx.N, Total: len(queries), TopN: topN}
	var rrSuma float64 // suma de reciprocal-rank (1/posición) para el ponderado
	for _, q := range queries {
		v := evaluar(q, eng.Buscar(q.texto()), topN, vault)
		switch v.Estado {
		case "ok":
			rep.OK++
		case "parcial":
			rep.Parcial++
		default:
			rep.Fallo++
		}
		if v.Posicion >= 1 {
			rrSuma += 1.0 / float64(v.Posicion) // MRR: descuento por rango (1/posición)
		}
		rep.Veredictos = append(rep.Veredictos, v)
	}
	if rep.Total > 0 {
		rep.EstrictoPct = 100 * float64(rep.OK) / float64(rep.Total)        // target en top-N (binario)
		rep.PonderadoPct = 100 * rrSuma / float64(rep.Total)                 // MRR: descuento por rango ~1/rank
	}

	salida, _ := json.MarshalIndent(rep, "", "  ")
	if err := os.WriteFile(reportPath, salida, 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "no pude escribir el reporte:", err)
		return 1
	}

	fmt.Printf("gspark eval [%s] — %s\n", eng.Nombre(), vault)
	fmt.Printf("  notas indexadas : %d\n", rep.Notas)
	fmt.Printf("  preguntas       : %d\n", rep.Total)
	for _, v := range rep.Veredictos {
		icono := map[string]string{"ok": "✅", "parcial": "⚠️ ", "fallo": "❌"}[v.Estado]
		pos := "-"
		if v.Posicion > 0 {
			pos = fmt.Sprintf("#%d", v.Posicion)
		}
		fmt.Printf("  %s %-4s %s (esperada %q en %s)\n", icono, v.ID, v.Pregunta, v.Esperada, pos)
	}
	fmt.Printf("  → estricto (top-%d): %.0f%% · ponderado: %.0f%%\n", topN, rep.EstrictoPct, rep.PonderadoPct)
	fmt.Printf("  reporte: %s\n", reportPath)
	return 0
}

func runSearch(args []string) int {
	if len(args) < 2 {
		fmt.Fprintln(os.Stderr, `uso: gspark search <vault> "<query>" [--entidad acme] [--engine bm25] [--contexto contexto] [--top 5]`)
		return 2
	}
	vault := args[0]
	query := args[1]
	ctxDir := flag(args, "--contexto", "contexto")
	engineName := flag(args, "--engine", "bm25")
	topN := 5
	if t := flag(args, "--top", "5"); t != "5" {
		fmt.Sscanf(t, "%d", &topN)
	}

	// Query operativa → apunta al puntero de la entidad, no busca en el índice.
	if esOperativa(query) {
		ent := entidadActiva(flag(args, "--entidad", ""))
		c, err := cargarContexto(ent, ctxDir)
		if err != nil {
			fmt.Printf("Query operativa detectada, pero no encuentro contexto/%s.json (%v).\n", ent, err)
			return 1
		}
		fmt.Print(tarjetaPuntero(c))
		return 0
	}

	// Query normal → retrieval sobre el cerebro.
	idx, err := construirIndice(vault)
	if err != nil {
		fmt.Fprintln(os.Stderr, "no pude indexar el vault:", err)
		return 1
	}
	eng := nuevoEngine(engineName, idx)
	res := eng.Buscar(query)
	fmt.Printf("gspark search [%s] — %q\n", eng.Nombre(), query)
	for i, r := range res {
		if i >= topN {
			break
		}
		fmt.Printf("  %d. %s\n", i+1, filepath.Base(r.Ruta))
	}
	if len(res) == 0 {
		fmt.Println("  (sin resultados)")
	}
	return 0
}

// runValidate confirma que CADA target del examen EXISTE en el vault → garantiza ceiling 100%.
// Falla RUIDOSO listando faltantes (nada de bajar el número en silencio).
func runValidate(args []string) int {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "uso: gspark validate <vault> [--queries eval/queries.json]")
		return 2
	}
	vault := args[0]
	queriesPath := flag(args, "--queries", "eval/queries.json")
	datos, err := os.ReadFile(queriesPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "no pude leer el examen:", err)
		return 1
	}
	var queries []Query
	if err := json.Unmarshal(datos, &queries); err != nil {
		fmt.Fprintln(os.Stderr, "el examen no es JSON válido:", err)
		return 1
	}
	idx, err := construirIndice(vault)
	if err != nil {
		fmt.Fprintln(os.Stderr, "no pude indexar el vault:", err)
		return 1
	}
	existe := make(map[string]bool, len(idx.Rutas))
	for _, r := range idx.Rutas {
		existe[rutaRelativa(r, vault)] = true
	}
	var faltantes, sinObjetivo []string
	conTarget := 0
	for _, q := range queries {
		objs := q.objetivos()
		if len(objs) == 0 {
			if q.Esperada == "" {
				sinObjetivo = append(sinObjetivo, q.ID)
			}
			continue // legacy por-substring: no se valida existencia
		}
		conTarget++
		for _, t := range objs {
			if !existe[t] {
				faltantes = append(faltantes, fmt.Sprintf("%s → %q", q.ID, t))
			}
		}
	}
	fmt.Printf("gspark validate — %s\n", queriesPath)
	fmt.Printf("  preguntas: %d · con target de identidad: %d · notas en vault: %d\n", len(queries), conTarget, idx.N)
	if len(sinObjetivo) > 0 {
		fmt.Printf("  ⚠️  sin target ni esperada (revisar): %s\n", strings.Join(sinObjetivo, ", "))
	}
	if len(faltantes) > 0 {
		fmt.Printf("  ❌ TARGETS INEXISTENTES (%d) — el ceiling NO es 100%%; corrígelos:\n", len(faltantes))
		for _, f := range faltantes {
			fmt.Printf("     · %s\n", f)
		}
		return 1
	}
	fmt.Println("  ✅ todos los targets existen → ceiling 100% garantizado.")
	return 0
}

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "uso: gspark <eval|search> …")
		os.Exit(2)
	}
	switch os.Args[1] {
	case "eval":
		os.Exit(runEval(os.Args[2:]))
	case "search":
		os.Exit(runSearch(os.Args[2:]))
	case "validate":
		os.Exit(runValidate(os.Args[2:]))
	case "mcp":
		os.Exit(runMCP(os.Args[2:]))
	default:
		fmt.Fprintf(os.Stderr, "comando desconocido: %q (usa: eval | search | validate | mcp)\n", os.Args[1])
		os.Exit(2)
	}
}
