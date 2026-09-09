package render

import (
	"github.com/EmmaMadrid/go-printer-agent/internal/config"
	"github.com/EmmaMadrid/go-printer-agent/internal/escpos"
)

// Kiosko arma el comprobante de orden del autoservicio: mismo transporte que la
// nota de venta, pero con el número de orden en grande (lo que el cliente lee de
// lejos), sin pago y con la indicación de pasar a caja.
func Kiosko(t KioskoTicket, n config.Notas) ([]byte, error) {
	w := nuevoWriter(n)

	encabezadoEmpresa(w, n, t.Empresa, false)

	w.Println("TU NUMERO DE ORDEN")
	w.Bold(true)
	w.SetTextSize(2, 2)
	w.Println(t.NumeroOrden)
	w.SetTextNormal()
	w.Bold(false)
	w.DrawLine()

	w.AlignLeft()
	kv(w, "Folio", t.Folio)
	kv(w, "Fecha", fhora(t.Fecha, false))
	w.DrawLine()

	colCant, colDesc, colImporte := columnasPartidas(n.Width)
	w.TableCustom([]escpos.Celda{
		{Texto: "Cant", Align: "LEFT", Cols: colCant, Bold: true},
		{Texto: "Descripción", Align: "LEFT", Cols: colDesc, Bold: true},
		{Texto: "Importe", Align: "RIGHT", Cols: colImporte, Bold: true},
	})
	w.DrawLine()
	for _, l := range t.Lineas {
		w.TableCustom([]escpos.Celda{
			{Texto: num(l.Cantidad), Align: "LEFT", Cols: colCant},
			{Texto: l.Descripcion, Align: "LEFT", Cols: colDesc},
			{Texto: mon(l.Importe), Align: "RIGHT", Cols: colImporte},
		})
	}
	w.DrawLine()

	kv(w, "Artículos", num(t.NumArticulos))
	w.Bold(true)
	w.LeftRight("TOTAL", mon(t.Total))
	w.Bold(false)
	w.DrawLine()

	w.AlignCenter()
	w.Bold(true)
	w.Println("PASA A CAJA A PAGAR")
	w.Bold(false)
	if t.TiempoEstimadoMin != 0 {
		w.Println("Tiempo estimado: " + num(t.TiempoEstimadoMin) + " min")
	}
	w.DrawLine()
	w.Println("Este documento no es un comprobante fiscal.")
	w.NewLine()
	w.Println("¡Gracias por tu preferencia!")
	w.NewLine()

	if t.Opciones.cortar(n) {
		w.Cut()
	}
	return w.Bytes(), nil
}
