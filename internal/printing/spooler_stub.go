//go:build !windows

package printing

import "fmt"

// El agente vive en las cajas Windows; en otros sistemas solo compila para que
// el desarrollo y las pruebas (render, HTTP, TCP) funcionen fuera de Windows.

// EnviarRaw no está disponible fuera de Windows: use una impresora de red.
func EnviarRaw(nombre string, datos []byte) error {
	return fmt.Errorf("la impresión por spooler solo está disponible en Windows; use conexion \"red\"")
}

// Listar no está disponible fuera de Windows.
func Listar() ([]string, error) { return []string{}, nil }

// Predeterminada no está disponible fuera de Windows.
func Predeterminada() string { return "" }

// Estado no está disponible fuera de Windows.
func Estado(nombre string) string { return "" }
