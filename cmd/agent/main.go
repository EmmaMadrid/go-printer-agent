// Command agent es el agente local de impresión: un solo ejecutable que hace
// de puente entre una app web y las impresoras de la PC.
//
// Un binario cubre todo el ciclo de vida —correr, instalarse como servicio,
// registrarse como tarea de sesión, diagnosticarse— para que instalar en una
// caja sea copiar un archivo y ejecutarlo.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/EmmaMadrid/go-printer-agent/internal/config"
	"github.com/EmmaMadrid/go-printer-agent/internal/httpapi"
	"github.com/EmmaMadrid/go-printer-agent/internal/printing"
	"github.com/EmmaMadrid/go-printer-agent/internal/winsvc"
)

// tamanoMaximoBitacora recorta la bitácora del servicio antes de que crezca
// sin control en una caja que lleva meses encendida.
const tamanoMaximoBitacora = 5 << 20

func main() {
	var (
		rutaConfig = flag.String("config", "", "ruta del config.json (por defecto, junto al ejecutable)")
		puerto     = flag.Int("port", 0, "puerto de escucha (pisa el del config.json)")
		host       = flag.String("host", "", "interfaz de escucha; 0.0.0.0 acepta la LAN")
	)
	flag.Usage = uso

	comando, args := separarComando(os.Args[1:])
	if err := flag.CommandLine.Parse(args); err != nil {
		os.Exit(2)
	}

	// El servicio no tiene consola; el resto de subcomandos sí deben poder
	// escribir en la terminal desde la que se los invocó.
	comoServicio := winsvc.EsServicio()
	if !comoServicio {
		winsvc.AdjuntarConsola()
	}
	log.SetFlags(log.Ldate | log.Ltime)

	config.Init(*rutaConfig)

	switch comando {
	case "run", "":
		correr(comoServicio, *puerto, *host)
	case "install":
		salirSi(instalarServicio())
	case "uninstall":
		salirSi(desinstalarServicio())
	case "start":
		salirSi(conElevacion(winsvc.Iniciar, "start"))
		fmt.Println("Servicio iniciado.")
	case "stop":
		salirSi(conElevacion(winsvc.Detener, "stop"))
		fmt.Println("Servicio detenido.")
	case "install-user":
		salirSi(instalarTarea())
	case "uninstall-user":
		salirSi(winsvc.DesinstalarTarea())
		fmt.Println("Tarea de inicio de sesión eliminada.")
	case "status":
		estado()
	case "printers":
		listarImpresoras()
	case "version":
		fmt.Printf("printer-agent %s\n", httpapi.Version)
	case "help", "-h", "--help":
		uso()
	default:
		fmt.Fprintf(os.Stderr, "Subcomando desconocido: %s\n\n", comando)
		uso()
		os.Exit(2)
	}
}

// separarComando extrae el primer argumento si no es una bandera, para poder
// escribir `agent install -config C:\ruta\config.json`.
func separarComando(args []string) (string, []string) {
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		return args[0], args[1:]
	}
	return "", args
}

func uso() {
	fmt.Fprintf(os.Stderr, `printer-agent %s — agente local de impresión ESC/POS y de hoja completa.

Uso: %s <subcomando> [banderas]

Subcomandos:
  run              Corre el agente en primer plano (por defecto).
  install          Lo instala como servicio de Windows (arranca con el equipo).
  uninstall        Quita el servicio.
  start | stop     Arranca o detiene el servicio ya instalado.
  install-user     Lo registra como tarea al iniciar sesión (sin permisos de
                   administrador; ve las impresoras del usuario y las de RDP).
  uninstall-user   Quita la tarea de inicio de sesión.
  status           Muestra el estado del servicio y de las impresoras.
  printers         Lista las impresoras instaladas en esta PC.
  version          Muestra la versión del agente.

Banderas:
`, httpapi.Version, filepath.Base(os.Args[0]))
	flag.PrintDefaults()
}

// ── Ejecución ──────────────────────────────────────────────────────────────

func correr(comoServicio bool, puerto int, host string) {
	cfg := config.Cargar()
	if puerto > 0 {
		cfg.Port = puerto
	}
	if host != "" {
		cfg.Host = host
	}

	if comoServicio {
		cerrar := abrirBitacora()
		defer cerrar()
	}

	srv := httpapi.NuevoServidor(cfg)

	arrancar := func() error {
		log.Printf("Printer agent %s escuchando en http://%s", httpapi.Version, srv.Addr)
		log.Printf("Config: %s", config.Ruta())
		log.Printf("Notas: %s (%s, %d columnas)", printing.DestinoDe(cfg.Notas).Etiqueta(), cfg.Notas.Type, cfg.Notas.Width)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("El servidor se detuvo: %v", err)
			return err
		}
		return nil
	}
	detener := func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := srv.Shutdown(ctx); err != nil {
			log.Printf("Cierre forzado: %v", err)
		}
	}

	if comoServicio {
		if err := winsvc.Correr(arrancar, detener); err != nil {
			log.Fatalf("El servicio terminó con error: %v", err)
		}
		return
	}

	// En consola: Ctrl+C cierra ordenadamente, sin cortar un ticket a la mitad.
	senales := make(chan os.Signal, 1)
	signal.Notify(senales, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-senales
		log.Println("Cerrando…")
		detener()
	}()

	if err := arrancar(); err != nil {
		os.Exit(1)
	}
}

// abrirBitacora manda el log a un archivo junto al ejecutable, porque un
// servicio de Windows no tiene a dónde escribir en pantalla.
func abrirBitacora() func() {
	ruta := filepath.Join(config.BaseDir(), "printer-agent.log")
	if st, err := os.Stat(ruta); err == nil && st.Size() > tamanoMaximoBitacora {
		os.Rename(ruta, ruta+".old")
	}
	f, err := os.OpenFile(ruta, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return func() {}
	}
	log.SetOutput(io.MultiWriter(f))
	return func() { f.Close() }
}

// ── Instalación ────────────────────────────────────────────────────────────

func instalarServicio() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	if err := conElevacion(func() error { return winsvc.Instalar(exe, "run") }, "install"); err != nil {
		return err
	}
	if err := winsvc.Iniciar(); err != nil {
		return fmt.Errorf("el servicio quedó instalado pero no arrancó: %w", err)
	}
	fmt.Printf("Servicio %q instalado y en ejecución.\n", winsvc.NombreLargo)
	fmt.Println("Configura la impresora desde el POS: Configuración → Impresoras.")
	return nil
}

func desinstalarServicio() error {
	if err := conElevacion(winsvc.Desinstalar, "uninstall"); err != nil {
		return err
	}
	fmt.Println("Servicio desinstalado.")
	return nil
}

func instalarTarea() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	if err := winsvc.InstalarTarea(exe); err != nil {
		return err
	}
	if err := winsvc.IniciarTarea(); err != nil {
		return fmt.Errorf("la tarea quedó registrada pero no arrancó: %w", err)
	}
	fmt.Println("Agente registrado para arrancar al iniciar sesión, y ya está corriendo.")
	return nil
}

// conElevacion corre una operación que exige permisos de administrador; si el
// proceso no los tiene, se relanza pidiéndolos por UAC y termina.
func conElevacion(fn func() error, subcomando string) error {
	if winsvc.Elevado() {
		return fn()
	}
	fmt.Println("Esta operación necesita permisos de administrador; se pedirá confirmación…")
	if err := winsvc.Reelevar([]string{subcomando}); err != nil {
		return fmt.Errorf("no se pudo elevar a administrador: %w", err)
	}
	os.Exit(0)
	return nil
}

// ── Diagnóstico ────────────────────────────────────────────────────────────

func estado() {
	cfg := config.Cargar()
	destino := printing.DestinoDe(cfg.Notas)

	fmt.Printf("printer-agent %s\n", httpapi.Version)
	fmt.Printf("Config:    %s\n", config.Ruta())
	fmt.Printf("Escucha:   http://%s:%d\n", cfg.Host, cfg.Port)

	if st, err := winsvc.Estado(); err == nil {
		fmt.Printf("Servicio:  %s\n", st)
	} else {
		fmt.Printf("Servicio:  %v\n", err)
	}

	fmt.Printf("Notas:     %s (%s, %d columnas)", destino.Etiqueta(), cfg.Notas.Type, cfg.Notas.Width)
	if !destino.EsRed() && destino.Printer != "" {
		fmt.Printf(" · estado: %s", printing.Estado(destino.Printer))
	}
	fmt.Println()

	carta := cfg.Carta.PrinterName
	if carta == "" {
		carta = "(predeterminada: " + printing.Predeterminada() + ")"
	}
	fmt.Printf("Carta:     %s\n", carta)
}

func listarImpresoras() {
	nombres, err := printing.Listar()
	if err != nil {
		fmt.Fprintf(os.Stderr, "No se pudieron listar las impresoras: %v\n", err)
		os.Exit(1)
	}
	if len(nombres) == 0 {
		fmt.Println("No hay impresoras instaladas en esta PC.")
		return
	}
	predeterminada := printing.Predeterminada()
	for _, n := range nombres {
		marca := " "
		if n == predeterminada {
			marca = "*"
		}
		fmt.Printf(" %s %s\n", marca, n)
	}
}

func salirSi(err error) {
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
