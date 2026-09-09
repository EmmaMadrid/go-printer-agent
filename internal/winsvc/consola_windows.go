//go:build windows

package winsvc

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"syscall"

	"golang.org/x/sys/windows"
)

// AdjuntarConsola engancha la salida del proceso a la consola desde la que se
// invocó, si la hay.
//
// El ejecutable se enlaza como aplicación de ventanas (-H windowsgui) para que
// la tarea de inicio de sesión no haga parpadear una consola negra en la
// pantalla de la caja. El precio es que los subcomandos de línea de comandos se
// quedarían mudos, y esto lo devuelve: al correr `agent status` desde una
// terminal, el texto aparece donde el usuario lo espera.
func AdjuntarConsola() {
	// Si la salida ya está conectada a algo —una tubería, un archivo, o una
	// consola heredada— no hay nada que hacer: engancharse a CONOUT$ aquí
	// desviaría el texto fuera de esa tubería y el llamador no vería nada.
	if salidaConectada() {
		ponerCodificacionUTF8()
		return
	}

	const adjuntarAlPadre = ^uintptr(0) // ATTACH_PARENT_PROCESS
	kernel32 := windows.NewLazySystemDLL("kernel32.dll")
	if r, _, _ := kernel32.NewProc("AttachConsole").Call(adjuntarAlPadre); r == 0 {
		return // no había consola padre: se ejecutó con doble clic
	}

	if f, err := os.OpenFile("CONOUT$", os.O_WRONLY, 0); err == nil {
		os.Stdout = f
		os.Stderr = f
		log.SetOutput(f)
	}
	ponerCodificacionUTF8()
}

// salidaConectada indica si el proceso ya tiene un destino válido para stdout.
func salidaConectada() bool {
	h, err := windows.GetStdHandle(windows.STD_OUTPUT_HANDLE)
	return err == nil && h != 0 && h != windows.InvalidHandle
}

// ponerCodificacionUTF8 hace que la consola muestre bien los acentos: Go emite
// UTF-8 y una consola en español suele venir en la página 850.
func ponerCodificacionUTF8() {
	const cpUTF8 = 65001
	kernel32 := windows.NewLazySystemDLL("kernel32.dll")
	kernel32.NewProc("SetConsoleOutputCP").Call(cpUTF8)
}

// ── Tarea de inicio de sesión ──────────────────────────────────────────────
//
// Alternativa al servicio para estaciones donde no hay permisos de
// administrador, o donde la impresora está instalada solo para un usuario (o
// llega redirigida por Escritorio Remoto): un servicio corre como LocalSystem y
// esas impresoras no existen para él, pero una tarea en la sesión del usuario
// sí las ve.

// InstalarTarea registra el agente para que arranque al iniciar sesión.
func InstalarTarea(rutaExe string) error {
	return schtasks("/Create",
		"/TN", Nombre,
		"/TR", fmt.Sprintf(`"%s" run`, rutaExe),
		"/SC", "ONLOGON",
		"/RL", "LIMITED",
		"/F",
	)
}

// DesinstalarTarea quita la tarea de inicio de sesión.
func DesinstalarTarea() error {
	return schtasks("/Delete", "/TN", Nombre, "/F")
}

// IniciarTarea arranca el agente ya mismo, sin esperar a reiniciar sesión.
func IniciarTarea() error {
	return schtasks("/Run", "/TN", Nombre)
}

func schtasks(args ...string) error {
	cmd := exec.Command("schtasks", args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	salida, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("schtasks falló: %v (%s)", err, salida)
	}
	return nil
}
