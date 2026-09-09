package render

import (
	"github.com/EmmaMadrid/go-printer-agent/internal/config"
	"github.com/EmmaMadrid/go-printer-agent/internal/escpos"
)

// Notas arma la nota de venta cobrada: encabezado de la empresa, datos de la
// cuenta, partidas, totales, formas de pago y pie.
func Notas(t TicketData, n config.Notas) ([]byte, error) {
	w := nuevoWriter(n)

	encabezadoEmpresa(w, n, t.Empresa, true)

	w.AlignLeft()
	kv(w, "Mesa", t.Mesa)
	if t.Personas != 0 {
		kv(w, "Personas", num(t.Personas))
	}
	kv(w, "Cliente", t.Cliente)
	kv(w, "Mesero", t.Mesero)
	kv(w, "Orden", t.Orden)
	kv(w, "Folio", t.Folio)
	kv(w, "Tipo", t.Tipo)
	kv(w, "Apertura", fhora(t.FechaApertura, false))
	kv(w, "Cierre", fhora(t.FechaCierre, false))
	kv(w, "Cajero", t.Cajero)
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
	kv(w, "Subtotal", mon(t.Subtotal))
	if t.Descuento != 0 {
		kv(w, "Descuento", "-"+mon(t.Descuento))
	}
	kv(w, "IVA incluido (16%)", mon(t.IvaIncluido))
	if t.Propina != 0 {
		kv(w, "Propina", mon(t.Propina))
	}
	w.Bold(true)
	w.LeftRight("TOTAL", mon(t.Total))
	w.Bold(false)
	w.DrawLine()

	for _, p := range t.Pagos {
		kv(w, "Pago - "+p.Metodo, mon(p.Monto))
	}
	if t.EfectivoRecibido != 0 {
		kv(w, "Efectivo entregado", mon(t.EfectivoRecibido))
	}
	if t.Cambio != 0 {
		kv(w, "Cambio", mon(t.Cambio))
	}
	w.DrawLine()

	w.AlignCenter()
	w.Println("Este documento no es un comprobante fiscal.")
	w.NewLine()
	w.Println("¡Gracias por su visita!")
	w.NewLine()
	if t.Software != "" {
		w.Println("Generado por " + t.Software)
	}
	w.Println(fhora(t.FechaCierre, true))
	w.NewLine()

	if t.Opciones.abrirCajon(n) {
		w.OpenCashDrawer()
	}
	if t.Opciones.cortar(n) {
		w.Cut()
	}
	return w.Bytes(), nil
}
