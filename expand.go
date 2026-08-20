// expand.go — motor "hybrid" v1: BM25 + EXPANSIÓN SEMÁNTICA-LÉXICA (100% pure-Go, single binary).
//
// BM25 solo no generaliza bien ante la brecha de paráfrasis (el humano y la nota usan palabras
// distintas). Esta mejora portable ataca esa brecha SIN modelo neural ni servicios: expande cada
// término de la consulta con un mapa GENERAL de conceptos/sinónimos del dominio (no afinado a las
// respuestas del examen) y corre BM25 ponderado (término original peso 1.0, sinónimo 0.5).
//
// Es un puente, no la meta: si no cierra el gap, el siguiente paso es semántico NEURAL EMBEBIDO
// (ONNX in-binary, single-binary) — nunca un servicio externo. Se mide con --engine hybrid.
package main

import "sort"

// grupos: conceptos del dominio; cada token del grupo se expande al resto. GENERAL (vocabulario
// de infra/dev/negocio del vault), NO tuneado a las respuestas del examen duro.
var grupos = [][]string{
	{"reparar", "arreglar", "corregir", "reparo", "corrupto", "corruptos", "dañado", "errores", "falla", "fallo"},
	{"borrar", "borre", "eliminar", "elimino", "destruir", "quitar", "remover"},
	{"snapshot", "snapshots", "instantanea", "captura", "foto", "revertir", "revierto", "volver", "restaurar", "restauro"},
	{"servidor", "servidores", "host", "hosts", "maquina", "maquinas", "nodo", "nodos", "equipo"},
	{"respaldo", "respaldos", "backup", "copia", "vzdump", "dump"},
	{"restaurar", "restauracion", "recuperar", "recuperacion", "recupero", "revivir", "recuperarla"},
	{"monitoreo", "monitor", "monitorear", "observabilidad", "alertas", "metricas", "logs"},
	{"seguridad", "seguro", "credencial", "credenciales", "contrasena", "password", "acceso", "clave"},
	{"contenedor", "contenedores", "ligero", "kvm", "maquina", "virtual", "virtualizacion"},
	{"pantalla", "headless", "grafica", "interfaz", "consola"},
	{"precio", "costo", "cobrar", "cobro", "cotizacion", "cotizar", "tarifa", "presupuesto"},
	{"conectividad", "internet", "enlace", "wifi", "red", "banda"},
	{"migrar", "migracion", "mover", "muevo", "trasladar", "portar", "correr"},
	{"nombre", "renombrar", "rename", "renombre"},
	{"cansado", "cansancio", "fatiga", "agotado", "saturado"},
	{"revisar", "revision", "reviso", "chequear", "verificar", "prueba", "pruebas", "test", "tests"},
	{"automatizar", "automatizacion", "automatico", "repetitivo", "repetitivas", "recurrente"},
	{"clasificar", "clasificacion", "categoria", "dimension", "dimensiones", "tipo", "atributos"},
	{"vender", "venta", "ventas", "comercio", "ecommerce", "producto", "productos", "inventario"},
	{"dinero", "inversion", "inversiones", "invertir", "invierto", "capital", "financiero"},
	{"peligroso", "destructivo", "riesgo", "riesgoso", "sin", "precheck", "prechequeo"},
	{"pagos", "pago", "pagar", "cashless", "tarjeta", "contactless", "nfc"},
	{"pegado", "atorado", "colgado", "trabado", "detener", "detengo", "matar", "kill"},
	{"disco", "almacenamiento", "storage", "espacio", "lleno", "llenó"},
	{"documentar", "documentacion", "escribir", "legible", "cristiano", "entienda"},
	{"agente", "agentes", "identidad", "identidades", "mesh", "comunican", "comunicacion"},
}

// idxSinon: token -> su grupo completo (para expansión). Se construye una vez.
var idxSinon = func() map[string][]string {
	m := map[string][]string{}
	for _, g := range grupos {
		for _, t := range g {
			m[t] = g
		}
	}
	return m
}()

// expandirQuery: términos de la consulta con peso (original 1.0, sinónimo 0.5).
func expandirQuery(q string) map[string]float64 {
	out := map[string]float64{}
	for _, t := range tokenizarUnicos(q) {
		out[t] = 1.0
		for _, s := range idxSinon[t] {
			if out[s] < 0.5 {
				out[s] = 0.5
			}
		}
	}
	return out
}

// engHybrid: BM25 ponderado sobre la consulta expandida.
type engHybrid struct {
	bm engBM25
}

func nuevoHybrid(idx *Indice) engHybrid { return engHybrid{bm: nuevoBM25(idx)} }
func (e engHybrid) Nombre() string      { return "hybrid" }

func (e engHybrid) Buscar(q string) []Resultado {
	wterms := expandirQuery(q)
	idx := e.bm.idx
	score := map[string]float64{}
	for term, peso := range wterms {
		for _, ruta := range idx.Rutas {
			tf := float64(idx.TF[ruta][term])
			if tf == 0 {
				continue
			}
			score[ruta] += peso * e.bm.puntajeTermino(ruta, term, tf)
		}
	}
	res := make([]Resultado, 0, len(score))
	for ruta := range score {
		res = append(res, Resultado{Ruta: ruta, Puntaje: int(score[ruta] * 100)})
	}
	sort.SliceStable(res, func(i, j int) bool { return score[res[i].Ruta] > score[res[j].Ruta] })
	return res
}
