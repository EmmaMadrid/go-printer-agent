// Command agent es el agente local de impresión: un solo ejecutable que hace
// de puente entre una app web y las impresoras de la PC.
//
// Un binario cubre todo el ciclo de vida —correr, instalarse como servicio,
// registrarse como tarea de sesión, diagnosticarse— para que instalar en una
// caja sea copiar un archivo y ejecutarlo.
package main

import (
	"bytes"
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

const titulo = "Sait Printer Agent"

// Salida para el usuario. Con consola es la terminal; sin consola (doble clic,
// o relanzado por UAC) se acumula y se muestra en una ventana al terminar,
// porque el ejecutable no tiene ventana propia y de otro modo quedaría mudo.
var (
	hayConsola bool
	salidaGUI  bytes.Buffer
	out        io.Writer = os.Stdout
)

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

	comoServicio := winsvc.EsServicio()
	if !comoServicio {
		hayConsola = winsvc.AdjuntarConsola()
		if !hayConsola {
			out = &salidaGUI
		}
	}
	log.SetFlags(log.Ldate | log.Ltime)

	config.Init(*rutaConfig)

	switch comando {
	case "":
		// Sin subcomando y sin consola = doble clic en el explorador. Nadie
		// quiere un proceso invisible: se guía al usuario a instalarlo.
		if !comoServicio && !hayConsola {
			dobleClic()
			terminar(0)
		}
		correr(comoServicio, *puerto, *host)
	case "run":
		correr(comoServicio, *puerto, *host)
	case "install":
		salirSi(instalarServicio())
	case "uninstall":
		salirSi(desinstalarServicio())
	case "start":
		salirSi(conElevacion(winsvc.Iniciar, "start"))
		decir("Servicio iniciado.")
	case "stop":
		salirSi(conElevacion(winsvc.Detener, "stop"))
		decir("Servicio detenido.")
	case "install-user":
		salirSi(instalarTarea())
	case "uninstall-user":
		salirSi(winsvc.DesinstalarTarea())
		decir("Tarea de inicio de sesión eliminada.")
	case "status":
		estado()
	case "printers":
		listarImpresoras()
	case "version":
		decir("printer-agent %s", httpapi.Version)
	case "help", "-h", "--help":
		uso()
	default:
		fmt.Fprintf(os.Stderr, "Subcomando desconocido: %s\n\n", comando)
		uso()
		terminar(2)
	}
	terminar(0)
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

// ── Salida al usuario ──────────────────────────────────────────────────────

func decir(formato string, a ...any) {
	fmt.Fprintf(out, formato+"\n", a...)
}

// terminar cierra el programa; sin consola, antes muestra lo acumulado en
// una ventana para que el usuario sepa qué pasó.
func terminar(codigo int) {
	if !hayConsola && salidaGUI.Len() > 0 {
		winsvc.Avisar(titulo, strings.TrimSpace(salidaGUI.String()), codigo != 0)
	}
	os.Exit(codigo)
}

func salirSi(err error) {
	if err != nil {
		decir("Error: %v", err)
		terminar(1)
	}
}

// dobleClic es lo que pasa al abrir el .exe desde el explorador: si el
// servicio ya está, se confirma y se abre el estado en el navegador; si no,
// se ofrece instalarlo ahí mismo.
func dobleClic() {
	cfg := config.Cargar()
	url := fmt.Sprintf("http://localhost:%d/status", cfg.Port)

	if st, err := winsvc.Estado(); err == nil && st == "en ejecución" {
		winsvc.Avisar(titulo, fmt.Sprintf(
			"El agente ya está instalado como servicio de Windows y en ejecución.\n\n%s\n\n"+
				"Configura la impresora desde el POS: Configuración → Impresoras.", url), false)
		winsvc.AbrirNavegador(url)
		return
	}

	if !winsvc.Preguntar(titulo,
		"El agente de impresión no está instalado como servicio.\n\n"+
			"¿Instalarlo ahora para que arranque solo con Windows?\n\n"+
			"(Se pedirá permiso de administrador.)") {
		return
	}
	salirSi(instalarServicio())
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
	return conElevacion(func() error {
		// Un agente abierto a mano estaría ocupando el puerto del servicio.
		winsvc.MatarInstanciasSueltas()
		if err := config.AsegurarArchivo(); err != nil {
			return fmt.Errorf("no se pudo crear config.json: %w", err)
		}
		if err := winsvc.Instalar(exe, "run"); err != nil {
			return err
		}
		if err := winsvc.Iniciar(); err != nil {
			return fmt.Errorf("el servicio quedó instalado pero no arrancó: %w", err)
		}
		cfg := config.Cargar()
		decir("Servicio %q instalado y en ejecución.", winsvc.NombreLargo)
		decir("Escucha en http://localhost:%d · config: %s", cfg.Port, config.Ruta())
		decir("Configura la impresora desde el POS: Configuración → Impresoras.")
		return nil
	}, "install")
}

func desinstalarServicio() error {
	if err := conElevacion(winsvc.Desinstalar, "uninstall"); err != nil {
		return err
	}
	decir("Servicio desinstalado.")
	return nil
}

func instalarTarea() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	winsvc.MatarInstanciasSueltas()
	if err := config.AsegurarArchivo(); err != nil {
		return fmt.Errorf("no se pudo crear config.json: %w", err)
	}
	if err := winsvc.InstalarTarea(exe); err != nil {
		return err
	}
	if err := winsvc.IniciarTarea(); err != nil {
		return fmt.Errorf("la tarea quedó registrada pero no arrancó: %w", err)
	}
	decir("Agente registrado para arrancar al iniciar sesión, y ya está corriendo.")
	return nil
}

// conElevacion corre una operación que exige permisos de administrador; si el
// proceso no los tiene, se relanza pidiéndolos por UAC y termina. El proceso
// elevado no tiene consola: reporta su resultado en una ventana.
func conElevacion(fn func() error, subcomando string) error {
	if winsvc.Elevado() {
		return fn()
	}
	if hayConsola {
		decir("Esta operación necesita permisos de administrador; se pedirá confirmación…")
	}
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

	decir("printer-agent %s", httpapi.Version)
	decir("Config:    %s", config.Ruta())
	decir("Escucha:   http://%s:%d", cfg.Host, cfg.Port)

	if st, err := winsvc.Estado(); err == nil {
		decir("Servicio:  %s", st)
	} else {
		decir("Servicio:  %v", err)
	}

	linea := fmt.Sprintf("Notas:     %s (%s, %d columnas)", destino.Etiqueta(), cfg.Notas.Type, cfg.Notas.Width)
	if !destino.EsRed() && destino.Printer != "" {
		linea += " · estado: " + printing.Estado(destino.Printer)
	}
	decir("%s", linea)

	carta := cfg.Carta.PrinterName
	if carta == "" {
		carta = "(predeterminada: " + printing.Predeterminada() + ")"
	}
	decir("Carta:     %s", carta)
}

func listarImpresoras() {
	nombres, err := printing.Listar()
	if err != nil {
		decir("No se pudieron listar las impresoras: %v", err)
		terminar(1)
	}
	if len(nombres) == 0 {
		decir("No hay impresoras instaladas en esta PC.")
		return
	}
	predeterminada := printing.Predeterminada()
	for _, n := range nombres {
		marca := " "
		if n == predeterminada {
			marca = "*"
		}
		decir(" %s %s", marca, n)
	}
}
