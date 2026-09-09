//go:build !windows

package pdf

import (
	"fmt"
	"os/exec"
)

func ocultarVentana(cmd *exec.Cmd) {}

func imprimirConShell(pdfPath string) error {
	return fmt.Errorf("imprimir a la predeterminada solo está disponible en Windows")
}
