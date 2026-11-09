package audit

import "fmt"

// knownActions es el catálogo central de acciones auditables. Un typo falla
// antes de escribir el evento.
var knownActions = map[string]struct{}{
	"admin.barridos":               {},
	"admin.verify_all":             {},
	"artefacto.eliminar":           {},
	"artefacto.publicar":           {},
	"bootstrap.humano":             {},
	"decision.crear":               {},
	"documento.crear":              {},
	"documento.sync":               {},
	"documento.version":            {},
	"epica.cerrar":                 {},
	"epica.crear":                  {},
	"epica.editar":                 {},
	"epic.mover_fase":              {},
	"fase.cerrar":                  {},
	"fase.crear":                   {},
	"fase.editar":                  {},
	"membresia.cambiar":            {},
	"membresia.quitar":             {},
	"mensaje.default_asumido":      {},
	"mensaje.descartar":            {},
	"mensaje.enviar":               {},
	"mensaje.estado":               {},
	"mensaje.resolver":             {},
	"mensaje.tomar":                {},
	"outbox.dead":                  {},
	"outbox.entregado":             {},
	"outbox.fallido":               {},
	"participante.baja":            {},
	"participante.crear":           {},
	"participante.editar":          {},
	"proyecto.crear":               {},
	"proyecto.editar":              {},
	"proyecto.importar":            {},
	"proyecto.pausar":              {},
	"proyecto.seguir":              {},
	"rechazo.auth.sesion_invalida": {},
	"rechazo.auth.sin_token":       {},
	"rechazo.auth.token_invalido":  {},
	"rechazo.permiso":              {},
	"rechazo.rate_limit":           {},
	"sesion.abrir":                 {},
	"ticket.claim":                 {},
	"ticket.claim_expirado":        {},
	"ticket.claim_liberar":         {},
	"ticket.avanzar":               {},
	"ticket.comentar":              {},
	"ticket.crear":                 {},
	"ticket.dependencia.crear":     {},
	"ticket.dependencia.quitar":    {},
	"ticket.documento.adjuntar":    {},
	"ticket.editar":                {},
	"ticket.revisar":               {},
	"ticket.revision.aprobar":      {},
	"ticket.revision.rechazar":     {},
	"token.emitir":                 {},
	"token.revocar":                {},
	"webhook.crear":                {},
	"webhook.quitar":               {},
}

func validateAction(action string) error {
	if _, ok := knownActions[action]; !ok {
		return fmt.Errorf("audit: acción desconocida %q", action)
	}
	return nil
}
