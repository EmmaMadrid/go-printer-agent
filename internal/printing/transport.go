package printing

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strconv"
	"sync"
	"time"

	"github.com/EmmaMadrid/go-printer-agent/internal/config"
)

// Destino es a dónde va un ticket: una impresora instalada en Windows (por
// nombre) o una térmica de red (por IP y puerto 9100).
type Destino struct {
	Conexion string // usb | red
	Printer  string
	Host     string
	Port     int
}

// DestinoDe traduce la configuración de notas a un destino concreto.
func DestinoDe(n config.Notas) Destino {
	d := Destino{Conexion: n.Conexion, Printer: n.PrinterName, Host: n.Red.Host, Port: n.Red.Port}
	if d.Port == 0 {
		d.Port = 9100
	}
	return d
}

// EsRed indica si el ticket sale por TCP en vez de por el spooler.
func (d Destino) EsRed() bool { return d.Conexion == "red" }

// Clave identifica la impresora física: los trabajos que comparten clave se
// serializan para que dos tickets no se entrelacen en el mismo papel.
func (d Destino) Clave() string {
	if d.EsRed() {
		return "red:" + net.JoinHostPort(d.Host, strconv.Itoa(d.Port))
	}
	return "usb:" + d.Printer
}

// Etiqueta es el nombre legible del destino, para respuestas y bitácora.
func (d Destino) Etiqueta() string {
	if d.EsRed() {
		return net.JoinHostPort(d.Host, strconv.Itoa(d.Port))
	}
	if d.Printer == "" {
		return "(sin configurar)"
	}
	return d.Printer
}

// Enviar entrega los bytes al destino que toque y marca como permanentes los
// fallos que no mejoran reintentando (impresora sin configurar o desinstalada).
func Enviar(d Destino, datos []byte) error {
	if d.EsRed() {
		return EnviarTCP(d.Host, d.Port, datos)
	}
	if d.Printer == "" {
		return Permanente(errors.New("no hay impresora térmica configurada en esta estación"))
	}
	err := EnviarRaw(d.Printer, datos)
	if err != nil && Estado(d.Printer) == "NOTFOUND" {
		return Permanente(err)
	}
	return err
}

// EnviarTCP abre un socket a la térmica de red y le vuelca el ticket. Es el
// camino preferido cuando el agente corre como servicio: no depende de que la
// impresora esté instalada en el perfil del usuario.
func EnviarTCP(host string, port int, datos []byte) error {
	if host == "" {
		return Permanente(errors.New("no hay dirección de impresora de red configurada"))
	}
	if port == 0 {
		port = 9100
	}
	dir := net.JoinHostPort(host, strconv.Itoa(port))

	conn, err := net.DialTimeout("tcp", dir, 6*time.Second)
	if err != nil {
		return fmt.Errorf("no se pudo conectar con la impresora %s: %w", dir, err)
	}
	defer conn.Close()

	if err := conn.SetWriteDeadline(time.Now().Add(6 * time.Second)); err != nil {
		return err
	}
	if _, err := conn.Write(datos); err != nil {
		return fmt.Errorf("falló el envío a %s: %w", dir, err)
	}
	return nil
}

// ── Diagnóstico ────────────────────────────────────────────────────────────

// Explicar traduce un fallo de impresión a un mensaje que el cajero entienda,
// consultando el estado real del dispositivo. Si no hay nada que aclarar,
// devuelve el error tal cual.
func Explicar(d Destino, err error) string {
	if err == nil {
		return ""
	}
	if d.EsRed() {
		return err.Error()
	}
	switch Estado(d.Printer) {
	case "NOTFOUND":
		return fmt.Sprintf("La impresora %q no está instalada en esta PC.", d.Printer)
	case "Offline":
		return fmt.Sprintf("La impresora %q está fuera de línea (¿equipo o servidor apagado?).", d.Printer)
	case "PaperOut":
		return fmt.Sprintf("La impresora %q se quedó sin papel.", d.Printer)
	case "PaperJam", "PaperProblem":
		return fmt.Sprintf("La impresora %q tiene un problema de papel.", d.Printer)
	case "DoorOpen":
		return fmt.Sprintf("La impresora %q tiene la tapa abierta.", d.Printer)
	case "NoToner":
		return fmt.Sprintf("La impresora %q se quedó sin tóner.", d.Printer)
	case "OutputBinFull":
		return fmt.Sprintf("La bandeja de salida de %q está llena.", d.Printer)
	case "UserIntervention":
		return fmt.Sprintf("La impresora %q necesita atención en el equipo.", d.Printer)
	case "Paused":
		return fmt.Sprintf("La impresora %q está en pausa en la cola de Windows.", d.Printer)
	case "Error":
		return fmt.Sprintf("La impresora %q reporta un error.", d.Printer)
	}
	return err.Error()
}

// ── Errores permanentes ────────────────────────────────────────────────────

// errPermanente marca un fallo que no mejora reintentando (configuración
// faltante, impresora inexistente): la cola se rinde de inmediato.
type errPermanente struct{ err error }

func (e errPermanente) Error() string { return e.err.Error() }
func (e errPermanente) Unwrap() error { return e.err }

// Permanente marca un error como no reintentable.
func Permanente(err error) error {
	if err == nil {
		return nil
	}
	return errPermanente{err}
}

// EsPermanente indica si el error ya no vale la pena reintentar.
func EsPermanente(err error) bool {
	var p errPermanente
	return errors.As(err, &p)
}

// ── Cola por impresora ─────────────────────────────────────────────────────

// Cola serializa los trabajos que van al mismo dispositivo y reintenta los
// fallos pasajeros (spooler ocupado, socket que se cayó). Dos cajas imprimiendo
// a la vez en la misma térmica ya no producen tickets entremezclados.
type Cola struct {
	mu       sync.Mutex
	porClave map[string]chan struct{}
	intentos int
	espera   time.Duration
}

// NuevaCola crea la cola con su política de reintentos.
func NuevaCola(intentos int, espera time.Duration) *Cola {
	if intentos < 1 {
		intentos = 1
	}
	return &Cola{
		porClave: make(map[string]chan struct{}),
		intentos: intentos,
		espera:   espera,
	}
}

// carril devuelve (creándolo si hace falta) el semáforo de un destino.
func (c *Cola) carril(clave string) chan struct{} {
	c.mu.Lock()
	defer c.mu.Unlock()
	l, ok := c.porClave[clave]
	if !ok {
		l = make(chan struct{}, 1)
		c.porClave[clave] = l
	}
	return l
}

// Ejecutar corre `fn` en exclusiva para ese destino, reintentando mientras el
// contexto lo permita. Devuelve el último error si nunca tuvo éxito.
func (c *Cola) Ejecutar(ctx context.Context, clave string, fn func() error) error {
	l := c.carril(clave)

	select {
	case l <- struct{}{}:
		defer func() { <-l }()
	case <-ctx.Done():
		return fmt.Errorf("la impresora %s sigue ocupada: %w", clave, ctx.Err())
	}

	var ultimo error
	for intento := 1; intento <= c.intentos; intento++ {
		if err := ctx.Err(); err != nil {
			if ultimo != nil {
				return ultimo
			}
			return err
		}
		ultimo = fn()
		if ultimo == nil {
			return nil
		}
		if EsPermanente(ultimo) || intento == c.intentos {
			return ultimo
		}
		select {
		case <-time.After(c.espera * time.Duration(intento)):
		case <-ctx.Done():
			return ultimo
		}
	}
	return ultimo
}
