// retrieval.go — índice del vault + motores de búsqueda intercambiables.
//
// El arnés de evaluación (main.go) llama a un Engine.Buscar(query); el motor es
// intercambiable con --engine keyword|bm25|hybrid, y el examen (queries.json) NO cambia.
// Todo stdlib: sin dependencias externas (binario portable, clave para la migración).
package main

import (
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Indice: representación del vault lista para rankear (BM25 necesita frecuencias y longitudes).
type Indice struct {
	Rutas  []string                  // orden estable de las notas
	Texto  map[string]string         // ruta -> texto en minúsculas (para keyword/embeddings)
	TF     map[string]map[string]int // ruta -> término -> frecuencia
	Len    map[string]int            // ruta -> nº de tokens
	DF     map[string]int            // término -> nº de documentos que lo contienen
	AvgLen float64                   // longitud promedio de documento
	N      int                       // nº de documentos
}

var stopwords = map[string]bool{
	"para": true, "como": true, "cuando": true, "donde": true, "porque": true,
	"pero": true, "esto": true, "esta": true, "este": true, "esos": true, "esas": true,
	"todo": true, "toda": true, "todos": true, "hacer": true, "tengo": true, "quiero": true,
	"sobre": true, "algo": true, "algun": true, "alguna": true, "otro": true, "otra": true,
	"desde": true, "hasta": true, "entre": true, "cada": true, "unos": true, "unas": true,
	"with": true, "from": true, "that": true, "this": true, "have": true, "what": true,
}

// tokenizar devuelve TODOS los tokens (con repetición) de longitud >= 4, sin stopwords.
func tokenizar(s string) []string {
	campos := strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return !((r >= 'a' && r <= 'z') || strings.ContainsRune("áéíóúñü", r))
	})
	out := campos[:0]
	for _, w := range campos {
		if len(w) >= 4 && !stopwords[w] {
			out = append(out, w)
		}
	}
	return out
}

// tokenizarUnicos: tokens distintos de una consulta (para keyword/BM25).
func tokenizarUnicos(s string) []string {
	vistos := map[string]bool{}
	var out []string
	for _, w := range tokenizar(s) {
		if !vistos[w] {
			vistos[w] = true
			out = append(out, w)
		}
	}
	return out
}

// construirIndice recorre el vault y arma el índice. Salta ocultos, _lab y node_modules.
func construirIndice(raiz string) (*Indice, error) {
	idx := &Indice{Texto: map[string]string{}, TF: map[string]map[string]int{}, Len: map[string]int{}, DF: map[string]int{}}
	err := filepath.WalkDir(raiz, func(ruta string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			n := d.Name()
			if (n != "." && strings.HasPrefix(n, ".")) || n == "_lab" || n == "node_modules" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(strings.ToLower(d.Name()), ".md") {
			return nil
		}
		// El doc del examen menciona TODOS los métodos → auto-contamina el ranking; se excluye.
		if strings.Contains(strings.ToLower(d.Name()), "baseline de retrieval") {
			return nil
		}
		datos, err := os.ReadFile(ruta)
		if err != nil {
			return nil
		}
		texto := strings.ToLower(string(datos))
		toks := tokenizar(texto)
		tf := map[string]int{}
		for _, t := range toks {
			tf[t]++
		}
		idx.Rutas = append(idx.Rutas, ruta)
		idx.Texto[ruta] = texto
		idx.TF[ruta] = tf
		idx.Len[ruta] = len(toks)
		for t := range tf {
			idx.DF[t]++
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	idx.N = len(idx.Rutas)
	var total int
	for _, l := range idx.Len {
		total += l
	}
	if idx.N > 0 {
		idx.AvgLen = float64(total) / float64(idx.N)
	}
	return idx, nil
}

// Engine: un motor de retrieval. Devuelve notas ordenadas por relevancia (mejor primero).
type Engine interface {
	Buscar(query string) []Resultado
	Nombre() string
}

// --- keyword: cobertura de términos + densidad (el v1). Base de comparación. ---
type engKeyword struct{ idx *Indice }

func (e engKeyword) Nombre() string { return "keyword" }
func (e engKeyword) Buscar(q string) []Resultado {
	palabras := tokenizarUnicos(q)
	type sc struct {
		ruta string
		cob  int
		dens float64
	}
	var tmp []sc
	for _, ruta := range e.idx.Rutas {
		texto := e.idx.Texto[ruta]
		hits, cob := 0, 0
		for _, w := range palabras {
			if c := strings.Count(texto, w); c > 0 {
				cob++
				hits += c
			}
		}
		if cob > 0 {
			tmp = append(tmp, sc{ruta, cob, float64(hits) / (float64(len(texto))/1000.0 + 1.0)})
		}
	}
	sort.SliceStable(tmp, func(i, j int) bool {
		if tmp[i].cob != tmp[j].cob {
			return tmp[i].cob > tmp[j].cob
		}
		return tmp[i].dens > tmp[j].dens
	})
	res := make([]Resultado, len(tmp))
	for i, t := range tmp {
		res[i] = Resultado{Ruta: t.ruta, Puntaje: t.cob}
	}
	return res
}

// --- BM25: el ranking de keyword de referencia (TF-IDF con saturación + normalización). ---
type engBM25 struct {
	idx    *Indice
	k1, bp float64
}

func nuevoBM25(idx *Indice) engBM25 { return engBM25{idx: idx, k1: 1.5, bp: 0.75} }
func (e engBM25) Nombre() string    { return "bm25" }

// puntajeTermino: contribución BM25 de UN término en UN documento (reusado por el híbrido).
func (e engBM25) puntajeTermino(ruta, term string, tf float64) float64 {
	df := float64(e.idx.DF[term])
	idf := math.Log(1 + (float64(e.idx.N)-df+0.5)/(df+0.5))
	dl := float64(e.idx.Len[ruta])
	return idf * (tf * (e.k1 + 1)) / (tf + e.k1*(1-e.bp+e.bp*dl/e.idx.AvgLen))
}

func (e engBM25) puntajes(q string) map[string]float64 {
	terms := tokenizarUnicos(q)
	out := map[string]float64{}
	for _, ruta := range e.idx.Rutas {
		var s float64
		for _, t := range terms {
			if tf := float64(e.idx.TF[ruta][t]); tf > 0 {
				s += e.puntajeTermino(ruta, t, tf)
			}
		}
		if s > 0 {
			out[ruta] = s
		}
	}
	return out
}

func (e engBM25) Buscar(q string) []Resultado {
	p := e.puntajes(q)
	res := make([]Resultado, 0, len(p))
	for ruta, s := range p {
		res = append(res, Resultado{Ruta: ruta, Puntaje: int(s * 100)})
	}
	sort.SliceStable(res, func(i, j int) bool { return p[res[i].Ruta] > p[res[j].Ruta] })
	return res
}

// nuevoEngine construye el motor pedido. Todo pure-Go, cero servicios externos (binario
// único portable). Un motor semántico futuro iría con embeddings EMBEBIDOS (ONNX in-binary),
// NUNCA un servicio aparte ni modelo neural externo — la portabilidad (single binary, offline) es innegociable.
func nuevoEngine(nombre string, idx *Indice) Engine {
	switch nombre {
	case "keyword":
		return engKeyword{idx: idx}
	case "hybrid":
		return nuevoHybrid(idx)
	default:
		return nuevoBM25(idx)
	}
}
