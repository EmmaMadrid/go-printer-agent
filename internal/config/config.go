// Package config carga y fusiona el config.json del agente.
//
// El archivo es OPCIONAL: solo actúa como respaldo cuando la app no manda el
// bloque `impresora` en el trabajo de impresión. Se relee en cada petición
// (igual que el agente JS), así que cambiarlo no obliga a reiniciar.
package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// Red es el destino TCP de una térmica de red (ESC/POS por el puerto 9100).
type Red struct {
	Host string `json:"host"`
	Port int    `json:"port"`
}

// Notas describe la impresora térmica (tickets, comandas, kiosko).
type Notas struct {
	Type          string `json:"type"`         // dialecto ESC/POS: epson | star | tanca | daruma
	Conexion      string `json:"conexion"`     // 'usb' (spooler por nombre) | 'red' (TCP)
	PrinterName   string `json:"printerName"`  // nombre de Windows si conexion = usb
	Red           Red    `json:"red"`          // destino si conexion = red
	Width         int    `json:"width"`        // columnas: 80 mm ≈ 42-48, 58 mm ≈ 32
	CharacterSet  string `json:"characterSet"` // PC858_EURO, WPC1252, …
	Cortar        bool   `json:"cortar"`
	AbrirCajon    bool   `json:"abrirCajon"`
	LogoPath      string `json:"logoPath"`
	LogoAnchoDots int    `json:"logoAnchoDots"`
}

// Carta describe la impresora de página completa (láser/inyección).
type Carta struct {
	PrinterName string `json:"printerName"` // '' = impresora predeterminada de Windows
	Paper       string `json:"paper"`
}

// Config es el contenido completo de config.json.
type Config struct {
	Port     int    `json:"port"`
	Host     string `json:"host"`     // '0.0.0.0' para aceptar la LAN (tablet del kiosko)
	Origenes string `json:"origenes"` // '*' o el dominio exacto de la app (CORS)
	Notas    Notas  `json:"notas"`
	Carta    Carta  `json:"carta"`
	PdfTool  string `json:"pdfTool"` // ruta a SumatraPDF/PDFtoPrinter ('' = autodetecta ./bin)
}

// Default devuelve la configuración de fábrica. Es también la base sobre la que
// se fusiona el JSON: los campos ausentes en el archivo conservan este valor,
// que es exactamente la semántica del spread del agente JS.
func Default() Config {
	return Config{
		Port:     9911,
		Host:     "127.0.0.1",
		Origenes: "*",
		Notas: Notas{
			Type:          "epson",
			Conexion:      "usb",
			Red:           Red{Port: 9100},
			Width:         42,
			CharacterSet:  "PC858_EURO",
			Cortar:        true,
			AbrirCajon:    true,
			LogoAnchoDots: 384,
		},
		Carta:   Carta{Paper: "letter"},
		PdfTool: "",
	}
}

var (
	mu      sync.RWMutex
	ruta    string // ruta resuelta de config.json
	baseDir string // carpeta de archivos sidecar (bin/, logs)
)

// Init fija la carpeta base y la ruta del config.json. `explicita` gana; si va
// vacía se busca junto al ejecutable y, si ahí no está, en el directorio actual
// (para que `go run` durante el desarrollo encuentre el archivo del repo).
func Init(explicita string) {
	mu.Lock()
	defer mu.Unlock()

	baseDir = "."
	if exe, err := os.Executable(); err == nil {
		baseDir = filepath.Dir(exe)
	}

	if explicita != "" {
		ruta = explicita
		if d := filepath.Dir(explicita); d != "" {
			baseDir = d
		}
		return
	}

	junto := filepath.Join(baseDir, "config.json")
	if _, err := os.Stat(junto); err == nil {
		ruta = junto
		return
	}
	if wd, err := os.Getwd(); err == nil {
		local := filepath.Join(wd, "config.json")
		if _, err := os.Stat(local); err == nil {
			ruta = local
			baseDir = wd
			return
		}
	}
	ruta = junto // no existe todavía; se usarán los valores por defecto
}

// BaseDir es la carpeta donde viven los archivos sidecar (bin/, logs, config).
func BaseDir() string {
	mu.RLock()
	defer mu.RUnlock()
	return baseDir
}

// Ruta es la ubicación del config.json que se está usando.
func Ruta() string {
	mu.RLock()
	defer mu.RUnlock()
	return ruta
}

// Cargar lee el config.json y lo fusiona sobre los valores por defecto. Un
// archivo ausente o inválido no es un error fatal: se devuelven los defaults.
func Cargar() Config {
	cfg := Default()
	raw, err := os.ReadFile(Ruta())
	if err != nil {
		return cfg
	}
	// Quita el BOM que deja Notepad al guardar en UTF-8.
	raw = []byte(strings.TrimPrefix(string(raw), "\ufeff"))
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return Default()
	}
	return cfg
}

// Guardar escribe la configuración en disco con sangría (editable a mano).
func Guardar(cfg Config) error {
	raw, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(Ruta(), append(raw, '\n'), 0o644)
}

// AsegurarArchivo crea el config.json con los valores de fábrica si todavía no
// existe, para que quien instala tenga a la vista qué se puede ajustar.
func AsegurarArchivo() error {
	if _, err := os.Stat(Ruta()); err == nil {
		return nil
	}
	return Guardar(Default())
}
