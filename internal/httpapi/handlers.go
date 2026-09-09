package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"path/filepath"
	"runtime"
	"strconv"

	"github.com/EmmaMadrid/go-printer-agent/internal/config"
	"github.com/EmmaMadrid/go-printer-agent/internal/discovery"
	"github.com/EmmaMadrid/go-printer-agent/internal/pdf"
	"github.com/EmmaMadrid/go-printer-agent/internal/printing"
	"github.com/EmmaMadrid/go-printer-agent/internal/render"
)

// raiz es la página de cortesía que ve quien abre la URL en el navegador.
func (a *Agente) raiz(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		fallo(w, http.StatusNotFound, "Ruta desconocida: "+r.URL.Path)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	fmt.Fprint(w, "Printer agent activo. GET /status · GET /health · GET /printers · GET /printers/network · POST /print · POST /pdf")
}

// status resume la configuración vigente. Es lo que el POS consulta para saber
// si hay agente y qué impresoras tiene asignadas la estación.
func (a *Agente) status(w http.ResponseWriter, r *http.Request) {
	cfg := config.Cargar()
	escribirJSON(w, http.StatusOK, map[string]any{
		"ok":      true,
		"version": Version,
		"notas": map[string]any{
			"conexion": cfg.Notas.Conexion,
			// El agente JS devolvía aquí el nombre crudo (cadena vacía si no hay
			// impresora elegida) y la pantalla de Configuración del POS cuenta
			// con eso; no se sustituye por una etiqueta.
			"impresora": impresoraDeNotas(cfg.Notas),
			"type":      cfg.Notas.Type,
			"width":     cfg.Notas.Width,
		},
		"carta": map[string]any{
			"impresora": nombreCarta(cfg.Carta.PrinterName),
			"navegador": pdf.BuscarNavegador() != "",
			"pdfTool":   nombreAyudante(cfg),
		},
	})
}

// version identifica el binario (útil al actualizar un parque de cajas).
func (a *Agente) version(w http.ResponseWriter, r *http.Request) {
	escribirJSON(w, http.StatusOK, map[string]any{
		"ok":      true,
		"version": Version,
		"runtime": runtime.Version(),
		"os":      runtime.GOOS + "/" + runtime.GOARCH,
	})
}

// health hace un diagnóstico completo de la estación y enumera lo que impediría
// imprimir. Es el endpoint que conviene mirar cuando alguien reporta "no sale
// el ticket": dice exactamente qué falta.
func (a *Agente) health(w http.ResponseWriter, r *http.Request) {
	cfg := config.Cargar()
	destino := printing.DestinoDe(cfg.Notas)
	navegador := pdf.BuscarNavegador()
	ayudante := pdf.BuscarAyudante(cfg.PdfTool, config.BaseDir())

	var problemas []string

	estado := ""
	if destino.EsRed() {
		if destino.Host == "" {
			problemas = append(problemas, "La impresora de notas está en modo red pero no tiene dirección IP configurada.")
		}
	} else if destino.Printer == "" {
		problemas = append(problemas, "No hay impresora térmica seleccionada en esta estación (Configuración → Impresoras en el POS).")
	} else {
		estado = printing.Estado(destino.Printer)
		if estado != "" && estado != "Normal" {
			problemas = append(problemas, printing.Explicar(destino, fmt.Errorf("estado %s", estado)))
		}
	}

	if navegador == "" {
		problemas = append(problemas, "No se encontró Edge ni Chrome: no se pueden imprimir documentos en hoja completa.")
	}
	if cfg.Carta.PrinterName != "" && ayudante == "" {
		problemas = append(problemas, "Falta SumatraPDF.exe o PDFtoPrinter.exe en la carpeta bin/ para imprimir hoja completa a una impresora específica.")
	}

	instaladas, err := printing.Listar()
	if err != nil {
		problemas = append(problemas, "No se pudo consultar la lista de impresoras de Windows: "+err.Error())
	}

	escribirJSON(w, http.StatusOK, map[string]any{
		"ok":      len(problemas) == 0,
		"version": Version,
		"config":  config.Ruta(),
		"notas": map[string]any{
			"conexion":  cfg.Notas.Conexion,
			"impresora": destino.Etiqueta(),
			"estado":    estado,
			"type":      cfg.Notas.Type,
			"width":     cfg.Notas.Width,
		},
		"carta": map[string]any{
			"impresora": nombreCarta(cfg.Carta.PrinterName),
			"navegador": navegador,
			"pdfTool":   nombreAyudante(cfg),
		},
		"impresorasInstaladas": len(instaladas),
		"predeterminada":       printing.Predeterminada(),
		"problemas":            problemas,
	})
}

// printers lista las impresoras instaladas, para armar el selector del POS.
func (a *Agente) printers(w http.ResponseWriter, r *http.Request) {
	nombres, err := printing.Listar()
	if err != nil {
		fallo(w, http.StatusInternalServerError, err.Error())
		return
	}
	escribirJSON(w, http.StatusOK, map[string]any{"ok": true, "printers": nombres})
}

// printersRed descubre térmicas de red tocando el puerto 9100 de la subred.
func (a *Agente) printersRed(w http.ResponseWriter, r *http.Request) {
	port, _ := strconv.Atoi(r.URL.Query().Get("port"))

	ctx, cancel := context.WithTimeout(r.Context(), limiteEscaneo)
	defer cancel()

	encontradas, err := discovery.Escanear(ctx, port)
	if err != nil {
		log.Printf("[scan] %v", err)
		fallo(w, http.StatusInternalServerError, err.Error())
		return
	}
	escribirJSON(w, http.StatusOK, map[string]any{"ok": true, "printers": encontradas})
}

// sobre es la envoltura mínima que hace falta para decidir qué plantilla usar
// sin comprometerse todavía con un tipo concreto.
type sobre struct {
	Tipo        string          `json:"tipo"`
	Bloques     json.RawMessage `json:"bloques"`
	Lineas      json.RawMessage `json:"lineas"`
	Impresora   json.RawMessage `json:"impresora"`
	HTML        string          `json:"html"`
	PrinterName string          `json:"printerName"`
	Paper       string          `json:"paper"`
}

// print es el único punto de entrada de impresión. El cuerpo decide la
// plantilla, en el mismo orden que resolvía el agente JS.
func (a *Agente) print(w http.ResponseWriter, r *http.Request) {
	crudo, err := io.ReadAll(r.Body)
	if err != nil {
		fallo(w, http.StatusBadRequest, "No se pudo leer el cuerpo de la petición: "+err.Error())
		return
	}
	var s sobre
	if err := json.Unmarshal(crudo, &s); err != nil {
		fallo(w, http.StatusBadRequest, "El cuerpo no es JSON válido: "+err.Error())
		return
	}

	cfg := config.Cargar()

	switch {
	case s.Tipo == "documento":
		a.imprimirDocumento(w, r, cfg, s)
	case esArreglo(s.Bloques):
		a.imprimirTermico(w, r, cfg, crudo, s, "ticket", func(n config.Notas) ([]byte, error) {
			var doc render.BloquesDoc
			if err := json.Unmarshal(crudo, &doc); err != nil {
				return nil, err
			}
			return render.Bloques(doc, n)
		})
	case s.Tipo == "comanda":
		if !esArreglo(s.Lineas) {
			fallo(w, http.StatusBadRequest, "Cuerpo inválido: falta la comanda.")
			return
		}
		a.imprimirTermico(w, r, cfg, crudo, s, "comanda", func(n config.Notas) ([]byte, error) {
			var c render.ComandaData
			if err := json.Unmarshal(crudo, &c); err != nil {
				return nil, err
			}
			return render.Comanda(c, n)
		})
	case s.Tipo == "kiosko":
		if !esArreglo(s.Lineas) {
			fallo(w, http.StatusBadRequest, "Cuerpo inválido: falta el ticket de kiosko.")
			return
		}
		a.imprimirTermico(w, r, cfg, crudo, s, "kiosko", func(n config.Notas) ([]byte, error) {
			var k render.KioskoTicket
			if err := json.Unmarshal(crudo, &k); err != nil {
				return nil, err
			}
			return render.Kiosko(k, n)
		})
	default:
		if !esArreglo(s.Lineas) {
			fallo(w, http.StatusBadRequest, "Cuerpo inválido: falta el ticket.")
			return
		}
		a.imprimirTermico(w, r, cfg, crudo, s, "notas", func(n config.Notas) ([]byte, error) {
			var t render.TicketData
			if err := json.Unmarshal(crudo, &t); err != nil {
				return nil, err
			}
			return render.Notas(t, n)
		})
	}
}

// imprimirTermico comparte el camino de las cuatro plantillas térmicas: fusiona
// la impresora del trabajo con la de la estación, arma los bytes y los encola.
func (a *Agente) imprimirTermico(
	w http.ResponseWriter, r *http.Request,
	cfg config.Config, crudo []byte, s sobre, rol string,
	armar func(config.Notas) ([]byte, error),
) {
	notas, err := notasEfectivas(cfg.Notas, s.Impresora)
	if err != nil {
		fallo(w, http.StatusBadRequest, "El bloque \"impresora\" del trabajo es inválido: "+err.Error())
		return
	}

	datos, err := armar(notas)
	if err != nil {
		fallo(w, http.StatusBadRequest, "No se pudo armar el ticket: "+err.Error())
		return
	}

	destino := printing.DestinoDe(notas)
	ctx, cancel := context.WithTimeout(r.Context(), limiteTermico)
	defer cancel()

	if err := a.cola.Ejecutar(ctx, destino.Clave(), func() error {
		return printing.Enviar(destino, datos)
	}); err != nil {
		log.Printf("[print] %s → %s: %v", rol, destino.Etiqueta(), err)
		fallo(w, http.StatusInternalServerError, printing.Explicar(destino, err))
		return
	}

	escribirJSON(w, http.StatusOK, map[string]any{
		"ok":        true,
		"rol":       rol,
		"via":       notas.Conexion,
		"bytes":     len(datos),
		"impresora": destino.Etiqueta(),
	})
}

// imprimirDocumento es la ruta de hoja completa: HTML → PDF → impresora.
func (a *Agente) imprimirDocumento(w http.ResponseWriter, r *http.Request, cfg config.Config, s sobre) {
	if s.HTML == "" {
		fallo(w, http.StatusBadRequest, "Falta html del documento.")
		return
	}
	printerName := s.PrinterName
	if printerName == "" {
		printerName = cfg.Carta.PrinterName
	}

	ctx, cancel := context.WithTimeout(r.Context(), limiteDocumento)
	defer cancel()

	clave := "carta:" + printerName
	var via string
	err := a.cola.Ejecutar(ctx, clave, func() error {
		var err error
		via, err = pdf.ImprimirHTML(ctx, s.HTML, printerName, pdf.BuscarAyudante(cfg.PdfTool, config.BaseDir()))
		return err
	})
	if err != nil {
		log.Printf("[print] carta → %s: %v", nombreCarta(printerName), err)
		fallo(w, http.StatusInternalServerError, err.Error())
		return
	}

	escribirJSON(w, http.StatusOK, map[string]any{
		"ok":        true,
		"rol":       "carta",
		"via":       via,
		"impresora": nombreCarta(printerName),
	})
}

// pdf convierte HTML a PDF y devuelve los bytes, para descargar o adjuntar.
func (a *Agente) pdf(w http.ResponseWriter, r *http.Request) {
	var cuerpo struct {
		HTML string `json:"html"`
	}
	if err := json.NewDecoder(r.Body).Decode(&cuerpo); err != nil {
		fallo(w, http.StatusBadRequest, "El cuerpo no es JSON válido: "+err.Error())
		return
	}
	if cuerpo.HTML == "" {
		fallo(w, http.StatusBadRequest, "Falta html.")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), limiteDocumento)
	defer cancel()

	datos, err := pdf.Bytes(ctx, cuerpo.HTML)
	if err != nil {
		log.Printf("[pdf] %v", err)
		fallo(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Length", strconv.Itoa(len(datos)))
	w.Write(datos)
}

// ── Apoyo ──────────────────────────────────────────────────────────────────

// notasEfectivas fusiona el bloque "impresora" del trabajo sobre la
// configuración de la estación: lo que la app manda gana, lo que omite se
// hereda del config.json.
func notasEfectivas(base config.Notas, override json.RawMessage) (config.Notas, error) {
	n := base
	if len(bytes.TrimSpace(override)) == 0 || string(bytes.TrimSpace(override)) == "null" {
		return n, nil
	}
	if err := json.Unmarshal(override, &n); err != nil {
		return base, err
	}
	if n.Red.Port == 0 {
		n.Red.Port = 9100
	}
	return n, nil
}

// esArreglo indica si el campo JSON venía como lista (el agente JS usaba
// Array.isArray para decidir la plantilla).
func esArreglo(raw json.RawMessage) bool {
	t := bytes.TrimSpace(raw)
	return len(t) > 0 && t[0] == '['
}

// impresoraDeNotas es el destino térmico tal como lo reportaba el agente JS:
// "host:puerto" en modo red, o el nombre de Windows (vacío si no hay ninguno).
func impresoraDeNotas(n config.Notas) string {
	d := printing.DestinoDe(n)
	if d.EsRed() {
		return d.Etiqueta()
	}
	return n.PrinterName
}

// nombreCarta es la etiqueta de la impresora de hoja completa.
func nombreCarta(nombre string) string {
	if nombre == "" {
		return "(predeterminada)"
	}
	return nombre
}

// nombreAyudante devuelve el archivo del ayudante de PDF, o nil si no hay
// (la app espera null, no cadena vacía).
func nombreAyudante(cfg config.Config) any {
	ruta := pdf.BuscarAyudante(cfg.PdfTool, config.BaseDir())
	if ruta == "" {
		return nil
	}
	return filepath.Base(ruta)
}
