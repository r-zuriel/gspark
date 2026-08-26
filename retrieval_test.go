package main

import (
    "reflect"
    "testing"
)

// TestTokenizar: verifica que tokenizar limpie el texto a palabras significativas.
func TestTokenizar(t *testing.T) {
    got := tokenizar("Reparar para el servidor caído")
    want := []string{"reparar", "servidor", "caído"}
    if !reflect.DeepEqual(got, want) {
        t.Errorf("tokenizar() = %v, quería %v", got, want)
    }
}
