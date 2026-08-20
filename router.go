// router.go — pre-filtro determinista: el sistema APUNTA al dato operativo, nunca lo GUARDA.
//
// Ante una query de clase-operativa (IP/host/puerto/credencial), gspark detecta la entidad
// activa y devuelve el PUNTERO definido en `contexto/<entidad>.json` — nunca el dato en sí.
// Cero LLM, cero servicios externos; corre delante del motor de retrieval. Config en JSON
// (Go stdlib). Por construcción no puede filtrar lo que no almacena.
package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Contexto: dónde vive lo operativo de una entidad. SOLO punteros — nunca IPs/credenciales.
type Contexto struct {
	Entidad   string                     `json:"entidad"`
	Operativo map[string]PunteroContexto `json:"operativo"`
}

type PunteroContexto struct {
	Donde  string `json:"donde"`
	Acceso string `json:"acceso,omitempty"`
}

// reOperativa: patrones de "dato operativo" que el índice NO debe almacenar.
var reOperativa = regexp.MustCompile(`(?i)\b(ip|host|hostname|puerto|port|credencial(es)?|contrase[nñ]a|password|c[oó]mo (accedo|entro|me conecto|ingreso)|d[oó]nde (est[aá]|vive) el servidor|acceso a[l]?|servidor de (base de datos|bd|bases))\b`)

func esOperativa(q string) bool { return reOperativa.MatchString(q) }

// entidadActiva: flag explícito > default. La entidad selecciona qué contexto/<entidad>.json
// se consulta; se pasa con --entidad.
func entidadActiva(flag string) string {
	if flag != "" {
		return strings.ToLower(flag)
	}
	return "default"
}

func cargarContexto(entidad, dir string) (*Contexto, error) {
	b, err := os.ReadFile(filepath.Join(dir, strings.ToLower(entidad)+".json"))
	if err != nil {
		return nil, err
	}
	var c Contexto
	if err := json.Unmarshal(b, &c); err != nil {
		return nil, err
	}
	return &c, nil
}

// tarjetaPuntero: la respuesta a una query operativa. Apunta a la fuente autorizada; nunca el dato.
func tarjetaPuntero(c *Contexto) string {
	var sb strings.Builder
	sb.WriteString("→ Dato operativo NO almacenado en el índice (por diseño).\n")
	sb.WriteString("  Entidad activa: " + strings.ToUpper(c.Entidad) + " — vive en:\n")
	for clase, p := range c.Operativo {
		sb.WriteString("   · " + clase + ": " + p.Donde)
		if p.Acceso != "" {
			sb.WriteString("  [acceso: " + p.Acceso + "]")
		}
		sb.WriteString("\n")
	}
	return sb.String()
}
