//go:build windows

package pdf

import (
	"fmt"
	"os/exec"
	"syscall"

	"golang.org/x/sys/windows"
)

// ocultarVentana evita que el navegador o el ayudante parpadeen una consola en
// la pantalla de la caja.
func ocultarVentana(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
}

// imprimirConShell usa el verbo "print" del visor de PDF registrado en Windows.
// Solo sirve para la impresora predeterminada: es el plan B cuando no hay
// ayudante en bin/.
func imprimirConShell(pdfPath string) error {
	verbo, err := windows.UTF16PtrFromString("print")
	if err != nil {
		return err
	}
	archivo, err := windows.UTF16PtrFromString(pdfPath)
	if err != nil {
		return err
	}
	if err := windows.ShellExecute(0, verbo, archivo, nil, nil, windows.SW_HIDE); err != nil {
		return fmt.Errorf("no se pudo imprimir a la impresora predeterminada: %w", err)
	}
	return nil
}
