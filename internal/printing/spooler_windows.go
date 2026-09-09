//go:build windows

// Package printing entrega los bytes a la impresora física.
//
// A diferencia del agente JS —que escribía un archivo temporal y lanzaba
// PowerShell para que compilara C# al vuelo y llamara a winspool— aquí se habla
// directo con el spooler de Windows. Sin proceso hijo, sin archivo intermedio y
// sin depender de la política de ejecución de scripts de la máquina.
package printing

import (
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	winspool = windows.NewLazySystemDLL("winspool.drv")

	procOpenPrinter       = winspool.NewProc("OpenPrinterW")
	procClosePrinter      = winspool.NewProc("ClosePrinter")
	procStartDocPrinter   = winspool.NewProc("StartDocPrinterW")
	procEndDocPrinter     = winspool.NewProc("EndDocPrinter")
	procStartPagePrinter  = winspool.NewProc("StartPagePrinter")
	procEndPagePrinter    = winspool.NewProc("EndPagePrinter")
	procWritePrinter      = winspool.NewProc("WritePrinter")
	procEnumPrinters      = winspool.NewProc("EnumPrintersW")
	procGetPrinter        = winspool.NewProc("GetPrinterW")
	procGetDefaultPrinter = winspool.NewProc("GetDefaultPrinterW")
)

// docInfo1 es la DOC_INFO_1W de winspool: describe el trabajo que se encola.
type docInfo1 struct {
	DocName    *uint16
	OutputFile *uint16
	Datatype   *uint16
}

// printerInfo4 es la PRINTER_INFO_4W: el nivel más barato para enumerar.
type printerInfo4 struct {
	PrinterName *uint16
	ServerName  *uint16
	Attributes  uint32
}

// printerInfo6 es la PRINTER_INFO_6: solo el estado del dispositivo.
type printerInfo6 struct {
	Status uint32
}

const (
	enumLocal       = 0x00000002
	enumConnections = 0x00000004

	// Estados de PRINTER_INFO_6 que le importan a una caja.
	statusPaused           = 0x00000001
	statusError            = 0x00000002
	statusPaperJam         = 0x00000008
	statusPaperOut         = 0x00000010
	statusPaperProblem     = 0x00000040
	statusOffline          = 0x00000080
	statusOutputBinFull    = 0x00000800
	statusNotAvailable     = 0x00001000
	statusUserIntervention = 0x00100000
	statusDoorOpen         = 0x00400000
	statusNoToner          = 0x00040000
)

// abrir toma un manejador de la impresora por nombre.
func abrir(nombre string) (windows.Handle, error) {
	p, err := windows.UTF16PtrFromString(nombre)
	if err != nil {
		return 0, fmt.Errorf("nombre de impresora inválido: %w", err)
	}
	var h windows.Handle
	r, _, e := procOpenPrinter.Call(uintptr(unsafe.Pointer(p)), uintptr(unsafe.Pointer(&h)), 0)
	if r == 0 {
		return 0, fmt.Errorf("no se pudo abrir la impresora %q: %w", nombre, e)
	}
	return h, nil
}

// EnviarRaw manda los bytes ESC/POS al spooler en modo RAW, por nombre de
// impresora. Es el camino "usb" (en realidad: cualquier impresora instalada en
// Windows, sea USB, de red o redirigida por RDP).
func EnviarRaw(nombre string, datos []byte) error {
	if nombre == "" {
		return fmt.Errorf("no hay impresora térmica configurada en esta estación")
	}
	if len(datos) == 0 {
		return fmt.Errorf("el trabajo de impresión venía vacío")
	}

	h, err := abrir(nombre)
	if err != nil {
		return err
	}
	defer procClosePrinter.Call(uintptr(h))

	docName, _ := windows.UTF16PtrFromString("POS Ticket")
	datatype, _ := windows.UTF16PtrFromString("RAW")
	di := docInfo1{DocName: docName, Datatype: datatype}

	job, _, e := procStartDocPrinter.Call(uintptr(h), 1, uintptr(unsafe.Pointer(&di)))
	if job == 0 {
		return fmt.Errorf("el spooler rechazó el trabajo en %q: %w", nombre, e)
	}
	defer procEndDocPrinter.Call(uintptr(h))

	if r, _, e := procStartPagePrinter.Call(uintptr(h)); r == 0 {
		return fmt.Errorf("StartPagePrinter falló en %q: %w", nombre, e)
	}
	defer procEndPagePrinter.Call(uintptr(h))

	var escritos uint32
	r, _, e := procWritePrinter.Call(
		uintptr(h),
		uintptr(unsafe.Pointer(&datos[0])),
		uintptr(len(datos)),
		uintptr(unsafe.Pointer(&escritos)),
	)
	if r == 0 {
		return fmt.Errorf("no se pudieron escribir los datos en %q: %w", nombre, e)
	}
	if int(escritos) != len(datos) {
		return fmt.Errorf("la impresora %q aceptó %d de %d bytes", nombre, escritos, len(datos))
	}
	return nil
}

// Listar devuelve las impresoras que ve esta sesión de Windows: locales y
// conectadas (incluidas las redirigidas por Escritorio Remoto).
func Listar() ([]string, error) {
	var necesario, devueltos uint32
	procEnumPrinters.Call(
		uintptr(enumLocal|enumConnections), 0, 4,
		0, 0,
		uintptr(unsafe.Pointer(&necesario)), uintptr(unsafe.Pointer(&devueltos)),
	)
	if necesario == 0 {
		return []string{}, nil
	}

	buf := make([]byte, necesario)
	r, _, e := procEnumPrinters.Call(
		uintptr(enumLocal|enumConnections), 0, 4,
		uintptr(unsafe.Pointer(&buf[0])), uintptr(necesario),
		uintptr(unsafe.Pointer(&necesario)), uintptr(unsafe.Pointer(&devueltos)),
	)
	if r == 0 {
		return nil, fmt.Errorf("no se pudieron enumerar las impresoras: %w", e)
	}

	infos := unsafe.Slice((*printerInfo4)(unsafe.Pointer(&buf[0])), devueltos)
	nombres := make([]string, 0, devueltos)
	for _, i := range infos {
		if i.PrinterName != nil {
			nombres = append(nombres, windows.UTF16PtrToString(i.PrinterName))
		}
	}
	return nombres, nil
}

// Predeterminada devuelve la impresora por defecto de Windows, o "" si no hay.
func Predeterminada() string {
	var n uint32
	procGetDefaultPrinter.Call(0, uintptr(unsafe.Pointer(&n)))
	if n == 0 {
		return ""
	}
	buf := make([]uint16, n)
	r, _, _ := procGetDefaultPrinter.Call(uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&n)))
	if r == 0 {
		return ""
	}
	return windows.UTF16ToString(buf)
}

// Estado consulta el estado del dispositivo. Devuelve la constante "NOTFOUND"
// cuando la impresora no está instalada, "" cuando no se pudo consultar y, si
// todo va bien, una descripción corta del problema (o "Normal").
func Estado(nombre string) string {
	if nombre == "" {
		return ""
	}
	h, err := abrir(nombre)
	if err != nil {
		return "NOTFOUND"
	}
	defer procClosePrinter.Call(uintptr(h))

	var necesario uint32
	procGetPrinter.Call(uintptr(h), 6, 0, 0, uintptr(unsafe.Pointer(&necesario)))
	if necesario == 0 {
		return ""
	}
	buf := make([]byte, necesario)
	r, _, _ := procGetPrinter.Call(uintptr(h), 6, uintptr(unsafe.Pointer(&buf[0])), uintptr(necesario), uintptr(unsafe.Pointer(&necesario)))
	if r == 0 {
		return ""
	}

	st := (*printerInfo6)(unsafe.Pointer(&buf[0])).Status
	switch {
	case st == 0:
		return "Normal"
	case st&statusOffline != 0 || st&statusNotAvailable != 0:
		return "Offline"
	case st&statusPaperOut != 0:
		return "PaperOut"
	case st&statusPaperJam != 0:
		return "PaperJam"
	case st&statusPaperProblem != 0:
		return "PaperProblem"
	case st&statusDoorOpen != 0:
		return "DoorOpen"
	case st&statusNoToner != 0:
		return "NoToner"
	case st&statusOutputBinFull != 0:
		return "OutputBinFull"
	case st&statusUserIntervention != 0:
		return "UserIntervention"
	case st&statusPaused != 0:
		return "Paused"
	case st&statusError != 0:
		return "Error"
	default:
		return "Normal"
	}
}
