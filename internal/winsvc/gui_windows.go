//go:build windows

package winsvc

import (
	"os"
	"os/exec"
	"strconv"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Cuando el ejecutable se abre con doble clic no hay consola donde escribir;
// la única forma de hablarle al usuario es una ventana. Estas funciones son el
// mínimo necesario para que "copiar el .exe y ejecutarlo" tenga sentido.

var (
	user32         = windows.NewLazySystemDLL("user32.dll")
	procMessageBox = user32.NewProc("MessageBoxW")
)

const (
	mbOK        = 0x00000000
	mbYesNo     = 0x00000004
	mbIconError = 0x00000010
	mbIconQuery = 0x00000020
	mbIconInfo  = 0x00000040
	mbSetFore   = 0x00010000
	mbTopmost   = 0x00040000
	idYes       = 6
)

func mensaje(titulo, texto string, flags uintptr) int {
	t, _ := windows.UTF16PtrFromString(titulo)
	m, _ := windows.UTF16PtrFromString(texto)
	r, _, _ := procMessageBox.Call(0, uintptr(unsafe.Pointer(m)), uintptr(unsafe.Pointer(t)), flags|mbSetFore|mbTopmost)
	return int(r)
}

// Avisar muestra un mensaje informativo (o de error) y espera a que lo cierren.
func Avisar(titulo, texto string, esError bool) {
	icono := uintptr(mbIconInfo)
	if esError {
		icono = mbIconError
	}
	mensaje(titulo, texto, mbOK|icono)
}

// Preguntar muestra una pregunta Sí/No y devuelve true si eligieron Sí.
func Preguntar(titulo, texto string) bool {
	return mensaje(titulo, texto, mbYesNo|mbIconQuery) == idYes
}

// AbrirNavegador abre una URL en el navegador predeterminado.
func AbrirNavegador(url string) {
	verbo, _ := windows.UTF16PtrFromString("open")
	destino, _ := windows.UTF16PtrFromString(url)
	windows.ShellExecute(0, verbo, destino, nil, nil, windows.SW_SHOWNORMAL)
}

// MatarInstanciasSueltas termina cualquier otro printer-agent.exe que esté
// corriendo (por ejemplo uno abierto con doble clic antes de instalar el
// servicio), para que el puerto quede libre. No toca al proceso actual.
func MatarInstanciasSueltas() {
	cmd := exec.Command("taskkill", "/F", "/IM", "printer-agent.exe", "/FI", "PID ne "+strconv.Itoa(os.Getpid()))
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	cmd.Run() // si no había ninguno, taskkill devuelve error y da igual
}
