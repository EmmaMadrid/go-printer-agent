// Package httpapi expone el agente por HTTP.
//
// El contrato es el mismo del agente JS que ya consumen el ERP, el kiosko y el
// comandero: mismas rutas, mismos cuerpos y mismas respuestas, para poder
// cambiar un ejecutable por el otro sin tocar una línea de la app.
package httpapi

import (
	"encoding/json"
	"log"
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/EmmaMadrid/go-printer-agent/internal/config"
	"github.com/EmmaMadrid/go-printer-agent/internal/printing"
)

// Version del agente; se puede fijar en el enlazado con -ldflags.
var Version = "2.0.0"

const (
	// tamañoMaximoCuerpo cubre un ticket con logotipo embebido como data URL.
	tamanoMaximoCuerpo = 8 << 20

	// Tiempos máximos de cada trabajo. Quedan por debajo de los que usa el
	// cliente (8 s térmica, 30 s documento) para poder responder un error
	// claro en vez de que al navegador se le venza la petición.
	limiteTermico   = 7 * time.Second
	limiteDocumento = 55 * time.Second
	limiteEscaneo   = 20 * time.Second
)

// Agente reúne el estado compartido entre peticiones.
type Agente struct {
	cola *printing.Cola
}

// NuevoAgente crea el agente con su cola de impresión (3 intentos, espera
// creciente: un spooler ocupado suele liberarse en menos de un segundo).
func NuevoAgente() *Agente {
	return &Agente{cola: printing.NuevaCola(3, 400*time.Millisecond)}
}

// Router arma el enrutador con CORS y límite de cuerpo ya aplicados.
func (a *Agente) Router() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /", a.raiz)
	mux.HandleFunc("GET /status", a.status)
	mux.HandleFunc("GET /version", a.version)
	mux.HandleFunc("GET /health", a.health)
	mux.HandleFunc("GET /printers", a.printers)
	mux.HandleFunc("GET /printers/network", a.printersRed)
	mux.HandleFunc("POST /print", a.print)
	mux.HandleFunc("POST /pdf", a.pdf)
	return cors(limitarCuerpo(mux))
}

// NuevoServidor arma el servidor HTTP listo para escuchar. Quien lo llama
// decide cuándo arrancarlo y cuándo cerrarlo (consola o servicio de Windows).
func NuevoServidor(cfg config.Config) *http.Server {
	a := NuevoAgente()
	return &http.Server{
		Addr:              net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port)),
		Handler:           a.Router(),
		ReadHeaderTimeout: 10 * time.Second,
		// Sin WriteTimeout: un documento carta puede tardar en renderizar y ya
		// lleva su propio límite por trabajo.
	}
}

// ── Middleware ─────────────────────────────────────────────────────────────

// cors deja que la app —incluso servida por https desde la nube— llame a este
// agente en localhost. La cabecera Private Network Access es la que Chrome y
// Edge exigen para un salto de red pública a red local, y tiene que ir también
// en la respuesta al preflight.
func cors(siguiente http.Handler) http.Handler {
	permitidos := config.Cargar().Origenes

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origen := r.Header.Get("Origin")
		switch {
		case permitidos == "" || permitidos == "*":
			if origen != "" {
				w.Header().Set("Access-Control-Allow-Origin", origen)
				w.Header().Add("Vary", "Origin")
			} else {
				w.Header().Set("Access-Control-Allow-Origin", "*")
			}
		default:
			w.Header().Set("Access-Control-Allow-Origin", permitidos)
			w.Header().Add("Vary", "Origin")
		}
		w.Header().Set("Access-Control-Allow-Private-Network", "true")

		if r.Method == http.MethodOptions {
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
			pedidas := r.Header.Get("Access-Control-Request-Headers")
			if pedidas == "" {
				pedidas = "Content-Type"
			}
			w.Header().Set("Access-Control-Allow-Headers", pedidas)
			w.Header().Set("Access-Control-Max-Age", "600")
			w.WriteHeader(http.StatusNoContent)
			return
		}
		siguiente.ServeHTTP(w, r)
	})
}

// limitarCuerpo evita que un cuerpo enorme agote la memoria de la caja.
func limitarCuerpo(siguiente http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, tamanoMaximoCuerpo)
		siguiente.ServeHTTP(w, r)
	})
}

// ── Respuestas ─────────────────────────────────────────────────────────────

func escribirJSON(w http.ResponseWriter, codigo int, cuerpo any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(codigo)
	if err := json.NewEncoder(w).Encode(cuerpo); err != nil {
		log.Printf("[http] no se pudo escribir la respuesta: %v", err)
	}
}

// fallo responde con la forma { ok: false, error } que espera la app.
func fallo(w http.ResponseWriter, codigo int, mensaje string) {
	escribirJSON(w, codigo, map[string]any{"ok": false, "error": mensaje})
}
