//go:build windows

// Package winsvc integra el agente con el Administrador de servicios de
// Windows sin envoltorios externos.
//
// El agente JS necesitaba descargar WinSW de internet para registrarse como
// servicio, lo que fallaba en cajas sin salida a la red o detrás de un proxy.
// Aquí el propio ejecutable es el servicio.
package winsvc

import (
	"fmt"
	"os"
	"strings"
	"time"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"
)

// Nombre es el identificador del servicio en services.msc.
const (
	Nombre      = "PrinterAgent"
	NombreLargo = "Sait Printer Agent"
	Descripcion = "Agente local de impresión ESC/POS y de documentos en hoja completa."
)

// EsServicio indica si el proceso lo arrancó el Administrador de servicios.
func EsServicio() bool {
	esServicio, err := svc.IsWindowsService()
	return err == nil && esServicio
}

// manejador adapta el ciclo de vida del servicio al del servidor HTTP.
type manejador struct {
	arrancar func() error
	detener  func()
}

func (m *manejador) Execute(args []string, pedidos <-chan svc.ChangeRequest, estados chan<- svc.Status) (bool, uint32) {
	const aceptados = svc.AcceptStop | svc.AcceptShutdown

	estados <- svc.Status{State: svc.StartPending}
	errores := make(chan error, 1)
	go func() { errores <- m.arrancar() }()
	estados <- svc.Status{State: svc.Running, Accepts: aceptados}

	for {
		select {
		case err := <-errores:
			// El servidor se cayó solo: reportarlo como fallo del servicio para
			// que Windows aplique la política de reinicio.
			estados <- svc.Status{State: svc.StopPending}
			if err != nil {
				return false, 1
			}
			return false, 0

		case c := <-pedidos:
			switch c.Cmd {
			case svc.Interrogate:
				estados <- c.CurrentStatus
			case svc.Stop, svc.Shutdown:
				estados <- svc.Status{State: svc.StopPending}
				m.detener()
				return false, 0
			default:
				estados <- c.CurrentStatus
			}
		}
	}
}

// Correr entrega el control al Administrador de servicios.
func Correr(arrancar func() error, detener func()) error {
	return svc.Run(Nombre, &manejador{arrancar: arrancar, detener: detener})
}

// ── Administración ─────────────────────────────────────────────────────────

// Elevado indica si el proceso tiene privilegios de administrador.
func Elevado() bool {
	var sid *windows.SID
	err := windows.AllocateAndInitializeSid(
		&windows.SECURITY_NT_AUTHORITY, 2,
		windows.SECURITY_BUILTIN_DOMAIN_RID, windows.DOMAIN_ALIAS_RID_ADMINS,
		0, 0, 0, 0, 0, 0, &sid,
	)
	if err != nil {
		return false
	}
	defer windows.FreeSid(sid)

	miembro, err := windows.Token(0).IsMember(sid)
	return err == nil && miembro
}

// Reelevar vuelve a lanzar este mismo ejecutable pidiendo permiso de
// administrador (el diálogo UAC) y devuelve para que el proceso actual termine.
func Reelevar(args []string) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	verbo, _ := windows.UTF16PtrFromString("runas")
	archivo, _ := windows.UTF16PtrFromString(exe)
	parametros, _ := windows.UTF16PtrFromString(strings.Join(args, " "))
	return windows.ShellExecute(0, verbo, archivo, parametros, nil, windows.SW_NORMAL)
}

// Instalar registra el servicio con arranque automático y reinicio ante fallos.
func Instalar(rutaExe string, args ...string) error {
	m, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("no se pudo abrir el Administrador de servicios: %w", err)
	}
	defer m.Disconnect()

	if s, err := m.OpenService(Nombre); err == nil {
		s.Close()
		return fmt.Errorf("el servicio %s ya está instalado; use \"uninstall\" primero", Nombre)
	}

	s, err := m.CreateService(Nombre, rutaExe, mgr.Config{
		DisplayName:  NombreLargo,
		Description:  Descripcion,
		StartType:    mgr.StartAutomatic,
		ErrorControl: mgr.ErrorNormal,
	}, args...)
	if err != nil {
		return fmt.Errorf("no se pudo crear el servicio: %w", err)
	}
	defer s.Close()

	// Si el proceso muere, que Windows lo levante en vez de dejar la caja sin
	// impresión hasta que alguien lo note.
	reinicios := []mgr.RecoveryAction{
		{Type: mgr.ServiceRestart, Delay: 5 * time.Second},
		{Type: mgr.ServiceRestart, Delay: 10 * time.Second},
		{Type: mgr.ServiceRestart, Delay: 30 * time.Second},
	}
	if err := s.SetRecoveryActions(reinicios, 86400); err != nil {
		return fmt.Errorf("servicio creado, pero no se pudo fijar el reinicio automático: %w", err)
	}
	return nil
}

// Desinstalar quita el servicio (deteniéndolo antes si hace falta).
func Desinstalar() error {
	m, err := mgr.Connect()
	if err != nil {
		return err
	}
	defer m.Disconnect()

	s, err := m.OpenService(Nombre)
	if err != nil {
		return fmt.Errorf("el servicio %s no está instalado", Nombre)
	}
	defer s.Close()

	if st, err := s.Query(); err == nil && st.State != svc.Stopped {
		if _, err := s.Control(svc.Stop); err == nil {
			esperarEstado(s, svc.Stopped, 20*time.Second)
		}
	}
	return s.Delete()
}

// Iniciar arranca el servicio ya instalado.
func Iniciar() error {
	s, m, err := abrirServicio()
	if err != nil {
		return err
	}
	defer m.Disconnect()
	defer s.Close()

	if st, err := s.Query(); err == nil && st.State == svc.Running {
		return nil
	}
	if err := s.Start(); err != nil {
		return fmt.Errorf("no se pudo iniciar el servicio: %w", err)
	}
	return esperarEstado(s, svc.Running, 20*time.Second)
}

// Detener para el servicio.
func Detener() error {
	s, m, err := abrirServicio()
	if err != nil {
		return err
	}
	defer m.Disconnect()
	defer s.Close()

	if _, err := s.Control(svc.Stop); err != nil {
		return fmt.Errorf("no se pudo detener el servicio: %w", err)
	}
	return esperarEstado(s, svc.Stopped, 20*time.Second)
}

// Estado describe en texto el estado actual del servicio.
//
// Se conecta con los permisos mínimos de consulta en vez de con acceso total,
// para que `status` funcione desde una terminal normal y no exija administrador.
func Estado() (string, error) {
	scm, err := windows.OpenSCManager(nil, nil, windows.SC_MANAGER_CONNECT)
	if err != nil {
		return "", fmt.Errorf("no se pudo consultar el Administrador de servicios: %w", err)
	}
	defer windows.CloseServiceHandle(scm)

	nombre, err := windows.UTF16PtrFromString(Nombre)
	if err != nil {
		return "", err
	}
	h, err := windows.OpenService(scm, nombre, windows.SERVICE_QUERY_STATUS)
	if err != nil {
		return "no instalado", nil
	}
	defer windows.CloseServiceHandle(h)

	var st windows.SERVICE_STATUS
	if err := windows.QueryServiceStatus(h, &st); err != nil {
		return "", err
	}
	switch svc.State(st.CurrentState) {
	case svc.Stopped:
		return "detenido", nil
	case svc.StartPending:
		return "iniciando", nil
	case svc.StopPending:
		return "deteniéndose", nil
	case svc.Running:
		return "en ejecución", nil
	case svc.Paused:
		return "en pausa", nil
	default:
		return fmt.Sprintf("estado %d", st.CurrentState), nil
	}
}

func abrirServicio() (*mgr.Service, *mgr.Mgr, error) {
	m, err := mgr.Connect()
	if err != nil {
		return nil, nil, err
	}
	s, err := m.OpenService(Nombre)
	if err != nil {
		m.Disconnect()
		return nil, nil, fmt.Errorf("el servicio %s no está instalado", Nombre)
	}
	return s, m, nil
}

func esperarEstado(s *mgr.Service, deseado svc.State, limite time.Duration) error {
	fin := time.Now().Add(limite)
	for time.Now().Before(fin) {
		st, err := s.Query()
		if err != nil {
			return err
		}
		if st.State == deseado {
			return nil
		}
		time.Sleep(300 * time.Millisecond)
	}
	return fmt.Errorf("el servicio no alcanzó el estado esperado a tiempo")
}
