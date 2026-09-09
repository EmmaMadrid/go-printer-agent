// Package pdf cubre la ruta de "página completa": convierte el HTML que manda
// la app en PDF con el Edge/Chrome que ya trae Windows y lo manda a una
// impresora láser o de inyección.
//
// No se embebe ningún motor de render: el navegador del sistema hace ese
// trabajo y el agente solo lo orquesta, igual que el agente JS.
package pdf

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const (
	esperaRender    = 25 * time.Second
	esperaEstable   = 5 * time.Second
	sondeoEstable   = 150 * time.Millisecond
	esperaImpresion = 30 * time.Second
)

// BuscarNavegador devuelve la ruta de Edge o Chrome, o "" si no hay ninguno.
func BuscarNavegador() string {
	candidatos := []string{
		filepath.Join(os.Getenv("ProgramFiles(x86)"), `Microsoft\Edge\Application\msedge.exe`),
		filepath.Join(os.Getenv("ProgramFiles"), `Microsoft\Edge\Application\msedge.exe`),
		filepath.Join(os.Getenv("ProgramFiles"), `Google\Chrome\Application\chrome.exe`),
		filepath.Join(os.Getenv("ProgramFiles(x86)"), `Google\Chrome\Application\chrome.exe`),
		filepath.Join(os.Getenv("LOCALAPPDATA"), `Google\Chrome\Application\chrome.exe`),
	}
	for _, c := range candidatos {
		if c == "" {
			continue
		}
		if st, err := os.Stat(c); err == nil && !st.IsDir() {
			return c
		}
	}
	return ""
}

var reAyudante = regexp.MustCompile(`(?i)(sumatra|pdftoprinter)`)

// BuscarAyudante localiza el programa que sabe mandar un PDF a una impresora
// por nombre. Gana la ruta configurada; si no, se busca en la carpeta bin/ del
// agente, aceptando nombres con versión (SumatraPDF-3.6.1-64.exe).
func BuscarAyudante(rutaConfigurada, baseDir string) string {
	if rutaConfigurada != "" {
		if st, err := os.Stat(rutaConfigurada); err == nil && !st.IsDir() {
			return rutaConfigurada
		}
	}
	bin := filepath.Join(baseDir, "bin")
	entradas, err := os.ReadDir(bin)
	if err != nil {
		return ""
	}
	var versionado string
	for _, e := range entradas {
		nombre := e.Name()
		if !strings.EqualFold(filepath.Ext(nombre), ".exe") {
			continue
		}
		base := strings.ToLower(strings.TrimSuffix(nombre, filepath.Ext(nombre)))
		if base == "sumatrapdf" || base == "pdftoprinter" {
			return filepath.Join(bin, nombre)
		}
		if versionado == "" && reAyudante.MatchString(nombre) {
			versionado = filepath.Join(bin, nombre)
		}
	}
	return versionado
}

// HTMLaPDF renderiza un archivo HTML local a PDF con el navegador del sistema.
func HTMLaPDF(ctx context.Context, htmlPath, pdfPath string) error {
	navegador := BuscarNavegador()
	if navegador == "" {
		return fmt.Errorf("no se encontró Edge ni Chrome para generar el PDF")
	}
	os.Remove(pdfPath)

	ctx, cancel := context.WithTimeout(ctx, esperaRender)
	defer cancel()

	perfil := filepath.Join(os.TempDir(), "printer-agent-edge-profile")
	url := "file:///" + strings.ReplaceAll(htmlPath, `\`, "/")

	cmd := exec.CommandContext(ctx, navegador,
		"--headless=new",
		"--disable-gpu",
		"--disable-extensions",
		"--no-first-run",
		"--no-pdf-header-footer",
		"--user-data-dir="+perfil,
		"--print-to-pdf="+pdfPath,
		url,
	)
	ocultarVentana(cmd)

	salida, err := cmd.CombinedOutput()
	if err != nil && ctx.Err() == nil {
		// El navegador puede devolver un código raro y aun así dejar el PDF.
		if _, statErr := os.Stat(pdfPath); statErr != nil {
			return fmt.Errorf("el navegador no pudo generar el PDF: %v (%s)", err, recortar(salida))
		}
	}

	// headless puede terminar antes de vaciar el archivo a disco: se espera a
	// que el tamaño se repita entre dos sondeos.
	if err := esperarArchivoEstable(pdfPath); err != nil {
		return fmt.Errorf("%w (%s)", err, recortar(salida))
	}
	return nil
}

// esperarArchivoEstable bloquea hasta que el PDF existe y deja de crecer.
func esperarArchivoEstable(ruta string) error {
	limite := time.Now().Add(esperaEstable)
	var anterior int64 = -1
	for time.Now().Before(limite) {
		st, err := os.Stat(ruta)
		if err == nil && st.Size() > 0 && st.Size() == anterior {
			return nil
		}
		if err == nil {
			anterior = st.Size()
		}
		time.Sleep(sondeoEstable)
	}
	if st, err := os.Stat(ruta); err == nil && st.Size() > 0 {
		return nil
	}
	return fmt.Errorf("se agotó el tiempo generando el PDF con el navegador")
}

// ImprimirPDF manda un PDF ya generado a la impresora indicada. Devuelve por
// qué vía se imprimió: "tool" (ayudante) o "default" (impresora predeterminada
// vía el visor de PDF registrado en Windows).
func ImprimirPDF(ctx context.Context, pdfPath, printerName, ayudante string) (string, error) {
	if ayudante != "" {
		ctx, cancel := context.WithTimeout(ctx, esperaImpresion)
		defer cancel()

		var args []string
		if strings.Contains(strings.ToLower(filepath.Base(ayudante)), "sumatra") {
			args = []string{"-print-to", printerName, "-silent", pdfPath}
			if printerName == "" {
				args = []string{"-print-to-default", "-silent", pdfPath}
			}
		} else {
			args = []string{pdfPath} // PDFtoPrinter: <pdf> ["<impresora>"]
			if printerName != "" {
				args = append(args, printerName)
			}
		}
		cmd := exec.CommandContext(ctx, ayudante, args...)
		ocultarVentana(cmd)
		if salida, err := cmd.CombinedOutput(); err != nil {
			return "", fmt.Errorf("%s falló: %v (%s)", filepath.Base(ayudante), err, recortar(salida))
		}
		return "tool", nil
	}

	if printerName != "" {
		return "", fmt.Errorf("para imprimir a una impresora específica en hoja completa, " +
			"coloca SumatraPDF.exe o PDFtoPrinter.exe en la carpeta bin/ del agente; " +
			"sin ese ayudante solo se puede usar la impresora predeterminada")
	}
	if err := imprimirConShell(pdfPath); err != nil {
		return "", err
	}
	return "default", nil
}

// ImprimirHTML es el camino completo: HTML → PDF → impresora.
func ImprimirHTML(ctx context.Context, html, printerName, ayudante string) (string, error) {
	htmlPath, pdfPath, limpiar, err := archivosTemporales(html)
	if err != nil {
		return "", err
	}
	defer limpiar()

	if err := HTMLaPDF(ctx, htmlPath, pdfPath); err != nil {
		return "", err
	}
	return ImprimirPDF(ctx, pdfPath, printerName, ayudante)
}

// Bytes renderiza el HTML y devuelve el PDF, para descargarlo o adjuntarlo.
func Bytes(ctx context.Context, html string) ([]byte, error) {
	htmlPath, pdfPath, limpiar, err := archivosTemporales(html)
	if err != nil {
		return nil, err
	}
	defer limpiar()

	if err := HTMLaPDF(ctx, htmlPath, pdfPath); err != nil {
		return nil, err
	}
	return os.ReadFile(pdfPath)
}

// archivosTemporales escribe el HTML y reserva la ruta del PDF hermano.
func archivosTemporales(html string) (htmlPath, pdfPath string, limpiar func(), err error) {
	base := filepath.Join(os.TempDir(), fmt.Sprintf("printer-agent-doc-%d", time.Now().UnixNano()))
	htmlPath = base + ".html"
	pdfPath = base + ".pdf"
	if err = os.WriteFile(htmlPath, []byte(html), 0o600); err != nil {
		return "", "", func() {}, fmt.Errorf("no se pudo preparar el documento: %w", err)
	}
	return htmlPath, pdfPath, func() {
		os.Remove(htmlPath)
		os.Remove(pdfPath)
	}, nil
}

// recortar deja la salida del proceso en algo que quepa en un mensaje de error.
func recortar(b []byte) string {
	s := strings.TrimSpace(string(b))
	if len(s) > 300 {
		s = s[:300] + "…"
	}
	return s
}
