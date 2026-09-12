//go:build !windows

// Package winsvc solo tiene implementación real en Windows. Estas versiones
// vacías permiten compilar y probar el resto del agente en otros sistemas.
package winsvc

import "fmt"

// Nombre es el identificador del servicio.
const (
	Nombre      = "SaitPrinterAgent"
	NombreLargo = "Sait Printer Agent"
	Descripcion = "Agente local de impresión ESC/POS y de documentos en hoja completa."
)

var errSoloWindows = fmt.Errorf("los servicios y tareas solo están disponibles en Windows")

// EsServicio siempre es falso fuera de Windows.
func EsServicio() bool { return false }

// AdjuntarConsola no hace nada fuera de Windows: siempre hay consola.
func AdjuntarConsola() bool { return true }

// Elevado siempre es falso fuera de Windows.
func Elevado() bool { return false }

// Correr no aplica fuera de Windows.
func Correr(arrancar func() error, detener func()) error { return errSoloWindows }

// Reelevar no aplica fuera de Windows.
func Reelevar(args []string) error { return errSoloWindows }

// Instalar no aplica fuera de Windows.
func Instalar(rutaExe string, args ...string) error { return errSoloWindows }

// Desinstalar no aplica fuera de Windows.
func Desinstalar() error { return errSoloWindows }

// Iniciar no aplica fuera de Windows.
func Iniciar() error { return errSoloWindows }

// Detener no aplica fuera de Windows.
func Detener() error { return errSoloWindows }

// Estado no aplica fuera de Windows.
func Estado() (string, error) { return "", errSoloWindows }

// InstalarTarea no aplica fuera de Windows.
func InstalarTarea(rutaExe string) error { return errSoloWindows }

// DesinstalarTarea no aplica fuera de Windows.
func DesinstalarTarea() error { return errSoloWindows }

// IniciarTarea no aplica fuera de Windows.
func IniciarTarea() error { return errSoloWindows }

// Avisar no aplica fuera de Windows: se imprime en la salida estándar.
func Avisar(titulo, texto string, esError bool) { fmt.Println(titulo + ": " + texto) }

// Preguntar no aplica fuera de Windows: siempre responde que no.
func Preguntar(titulo, texto string) bool { return false }

// AbrirNavegador no aplica fuera de Windows.
func AbrirNavegador(url string) {}

// MatarInstanciasSueltas no aplica fuera de Windows.
func MatarInstanciasSueltas() {}
