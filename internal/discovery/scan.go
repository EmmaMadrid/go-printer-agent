// Package discovery busca impresoras térmicas en la red local.
//
// No hace falta ningún protocolo de descubrimiento: una térmica de red escucha
// ESC/POS en el puerto 9100, así que basta con tocar ese puerto en cada host de
// las subredes /24 a las que está conectado el agente.
package discovery

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"sync"
	"time"
)

// Impresora es un candidato hallado en la red.
type Impresora struct {
	Host string `json:"host"`
	Port int    `json:"port"`
}

// PuertoPorDefecto es el puerto RAW estándar de las impresoras de red.
const PuertoPorDefecto = 9100

const (
	esperaPorHost = 400 * time.Millisecond
	enParalelo    = 128
)

// Escanear recorre las subredes /24 locales probando el puerto indicado y
// devuelve los hosts que responden, ordenados por subred.
func Escanear(ctx context.Context, port int) ([]Impresora, error) {
	if port <= 0 {
		port = PuertoPorDefecto
	}
	bases, err := subredesLocales()
	if err != nil {
		return nil, err
	}

	var (
		mu          sync.Mutex
		encontradas []Impresora
		wg          sync.WaitGroup
	)
	sem := make(chan struct{}, enParalelo)

	for _, base := range bases {
		for i := 1; i <= 254; i++ {
			host := fmt.Sprintf("%s%d", base, i)
			wg.Add(1)
			go func() {
				defer wg.Done()
				select {
				case sem <- struct{}{}:
					defer func() { <-sem }()
				case <-ctx.Done():
					return
				}
				if !puertoAbierto(ctx, host, port) {
					return
				}
				mu.Lock()
				encontradas = append(encontradas, Impresora{Host: host, Port: port})
				mu.Unlock()
			}()
		}
	}
	wg.Wait()

	if encontradas == nil {
		encontradas = []Impresora{}
	}
	return encontradas, nil
}

// subredesLocales deriva los prefijos "a.b.c." de cada interfaz IPv4 activa con
// máscara /24, que es prácticamente toda red de restaurante.
func subredesLocales() ([]string, error) {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil, err
	}
	var bases []string
	vistas := map[string]bool{}

	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, a := range addrs {
			ipnet, ok := a.(*net.IPNet)
			if !ok {
				continue
			}
			ip4 := ipnet.IP.To4()
			if ip4 == nil {
				continue
			}
			if ones, bits := ipnet.Mask.Size(); ones != 24 || bits != 32 {
				continue
			}
			base := fmt.Sprintf("%d.%d.%d.", ip4[0], ip4[1], ip4[2])
			if !vistas[base] {
				vistas[base] = true
				bases = append(bases, base)
			}
		}
	}
	return bases, nil
}

// puertoAbierto intenta una conexión corta; si el TCP acepta, hay algo escuchando.
func puertoAbierto(ctx context.Context, host string, port int) bool {
	d := net.Dialer{Timeout: esperaPorHost}
	conn, err := d.DialContext(ctx, "tcp", net.JoinHostPort(host, strconv.Itoa(port)))
	if err != nil {
		return false
	}
	conn.Close()
	return true
}
