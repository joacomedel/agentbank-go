package httpapi

import (
	"net/http"
	"strings"
)

type opDef struct {
	Method   string
	Path     string
	Tag      string
	Summary  string
	Body     string
	Response string
	Query    []string
}

func openAPIDocument() map[string]any {
	paths := map[string]any{}
	for _, op := range openAPIOperations() {
		item, _ := paths[op.Path].(map[string]any)
		if item == nil {
			item = map[string]any{}
			paths[op.Path] = item
		}
		method := strings.ToLower(op.Method)
		responseSchema := op.Response
		if responseSchema == "" {
			responseSchema = "FreeObject"
		}
		responses := map[string]any{
			"200": map[string]any{"description": "OK", "content": jsonContent(responseSchema)},
			"400": errorResponse("JSON inválido o body inválido"),
			"401": errorResponse("Token faltante, inválido o revocado"),
			"403": errorResponse("Sin permiso"),
			"404": errorResponse("Recurso inexistente"),
			"409": errorResponse("Conflicto de versión, claim, unicidad o idempotencia"),
			"422": errorResponse("Validación fallida o secreto detectado"),
			"429": errorResponse("Rate limit"),
			"500": errorResponse("Error interno"),
		}
		if op.Method == http.MethodPost {
			responses["201"] = map[string]any{"description": "Creado", "content": jsonContent(responseSchema)}
		}
		security := []map[string]any{{"bearerAuth": []any{}}}
		if op.Path == "/healthz" || op.Path == "/v1/meta" || op.Path == "/openapi.json" || op.Path == "/v1/openapi.json" {
			security = []map[string]any{}
		}
		operation := map[string]any{
			"tags":        []string{op.Tag},
			"summary":     op.Summary,
			"operationId": operationID(op.Method, op.Path),
			"responses":   responses,
			"security":    security,
		}
		params := pathParameters(op.Path)
		for _, query := range op.Query {
			params = append(params, map[string]any{
				"name": query, "in": "query", "required": false,
				"schema": map[string]any{"type": "string"},
			})
		}
		if len(params) > 0 {
			operation["parameters"] = params
		}
		if op.Body != "" {
			operation["requestBody"] = map[string]any{
				"required": true,
				"content":  jsonContent(op.Body),
			}
		}
		item[method] = operation
	}
	return map[string]any{
		"openapi": "3.0.3",
		"info": map[string]any{
			"title":       "AgentBank Go API",
			"version":     "0.2.0",
			"description": "Backend HTTP de AgentBank Go. Sin UI: la UI/Swagger aparte consume este contrato.",
		},
		"servers": []map[string]any{{"url": "http://127.0.0.1:8090", "description": "Local"}},
		"tags": []map[string]any{
			{"name": "Salud"}, {"name": "Identidad"}, {"name": "Proyectos"}, {"name": "Tickets"},
			{"name": "Fases"}, {"name": "Épicas"}, {"name": "Mensajes"}, {"name": "Documentos"},
			{"name": "Búsqueda"}, {"name": "Auditoría"}, {"name": "Operación"},
		},
		"components": map[string]any{
			"securitySchemes": map[string]any{
				"bearerAuth": map[string]any{"type": "http", "scheme": "bearer"},
			},
			"schemas": openAPISchemas(),
		},
		"paths": paths,
	}
}

func openAPIOperations() []opDef {
	return []opDef{
		{"GET", "/healthz", "Salud", "Health check", "", "Health", nil},
		{"GET", "/v1/meta", "Salud", "Metadatos del servidor", "", "Meta", nil},
		{"GET", "/openapi.json", "Salud", "Especificación OpenAPI", "", "FreeObject", nil},
		{"GET", "/v1/openapi.json", "Salud", "Especificación OpenAPI", "", "FreeObject", nil},

		{"GET", "/v1/me", "Identidad", "Participante autenticado y membresías", "", "Me", []string{"proyecto"}},
		{"GET", "/v1/participants", "Identidad", "Listar participantes", "", "ParticipantList", []string{"limit", "cursor", "activos"}},
		{"POST", "/v1/participants", "Identidad", "Crear participante y emitir token", "ParticipantCreate", "ParticipantCreated", nil},
		{"POST", "/v1/participants/{id}/tokens", "Identidad", "Rotar token", "", "TokenCreated", nil},
		{"POST", "/v1/participants/{id}/baja", "Identidad", "Dar de baja participante", "", "FreeObject", nil},
		{"POST", "/v1/tokens/{id}/revoke", "Identidad", "Revocar token", "", "FreeObject", nil},
		{"POST", "/v1/me/heartbeat", "Identidad", "Heartbeat explícito", "", "FreeObject", nil},

		{"GET", "/v1/projects", "Proyectos", "Listar proyectos legibles", "", "ProjectList", []string{"limit", "cursor"}},
		{"POST", "/v1/projects", "Proyectos", "Crear proyecto", "ProjectCreate", "Project", nil},
		{"GET", "/v1/projects/{id}", "Proyectos", "Obtener proyecto por id o slug", "", "Project", nil},
		{"PATCH", "/v1/projects/{id}", "Proyectos", "Editar metadatos del proyecto", "ProjectUpdate", "Project", nil},
		{"POST", "/v1/projects/{id}/pausa", "Proyectos", "Pausar cola del proyecto", "", "FreeObject", nil},
		{"POST", "/v1/projects/{id}/seguir", "Proyectos", "Reanudar cola del proyecto", "", "FreeObject", nil},
		{"GET", "/v1/projects/{id}/members", "Proyectos", "Listar miembros", "", "MemberList", []string{"limit", "cursor"}},
		{"PUT", "/v1/projects/{id}/members/{participant_id}", "Proyectos", "Crear o actualizar membresía", "MembershipUpdate", "Member", nil},
		{"DELETE", "/v1/projects/{id}/members/{participant_id}", "Proyectos", "Quitar membresía", "", "FreeObject", nil},
		{"GET", "/v1/projects/{id}/roles", "Proyectos", "Listar catálogo de roles", "", "RoleList", []string{"limit", "cursor"}},

		{"GET", "/v1/projects/{id}/tickets", "Tickets", "Listar tickets", "", "TicketList", []string{"limit", "cursor", "status", "priority", "role", "assignee_id", "phase_id", "epic_id", "key"}},
		{"POST", "/v1/projects/{id}/tickets", "Tickets", "Crear ticket", "TicketCreate", "Ticket", nil},
		{"GET", "/v1/tickets/{id}", "Tickets", "Obtener ticket por UUID, PREFIX-N o key", "", "Ticket", nil},
		{"PATCH", "/v1/tickets/{id}", "Tickets", "Editar ticket con versión", "TicketUpdate", "Ticket", nil},
		{"POST", "/v1/tickets/{id}/avanzar", "Tickets", "Avanzar/cerrar ticket con nota y pregunta", "TicketAdvance", "AdvanceResult", nil},
		{"POST", "/v1/tickets/{id}/claims", "Tickets", "Reclamar ticket", "", "ClaimResult", nil},
		{"POST", "/v1/tickets/{id}/claims/release", "Tickets", "Liberar claim", "", "Ticket", nil},
		{"POST", "/v1/tickets/{id}/comments", "Tickets", "Agregar comentario/nota/cierre", "CommentCreate", "Comment", nil},
		{"GET", "/v1/tickets/{id}/comments", "Tickets", "Listar comentarios", "", "CommentList", []string{"limit", "cursor"}},
		{"GET", "/v1/tickets/{id}/timeline", "Tickets", "Timeline del ticket", "", "TimelineList", []string{"limit"}},
		{"POST", "/v1/tickets/{id}/dependencies", "Tickets", "Agregar dependencia", "DependencyCreate", "Dependency", nil},
		{"DELETE", "/v1/tickets/{id}/dependencies/{depends_on}", "Tickets", "Quitar dependencia", "", "FreeObject", nil},
		{"POST", "/v1/tickets/siguiente", "Tickets", "Siguiente ticket de la cola", "TicketNext", "TicketNextResult", nil},
		{"GET", "/v1/tickets/listas", "Tickets", "Candidatos sin reclamar (compatibilidad)", "", "TicketList", []string{"project_id", "role", "phase_id", "epic_id"}},
		{"POST", "/v1/tickets/{id}/documents", "Tickets", "Adjuntar documento a ticket", "AttachDocument", "FreeObject", nil},
		{"GET", "/v1/tickets/{id}/documents", "Tickets", "Listar documentos del ticket", "", "DocumentList", nil},

		{"GET", "/v1/projects/{id}/phases", "Fases", "Listar fases", "", "PhaseList", nil},
		{"POST", "/v1/projects/{id}/phases", "Fases", "Crear fase", "PhaseCreate", "Phase", nil},
		{"GET", "/v1/phases/{id}", "Fases", "Obtener fase con épicas y tickets", "", "Phase", nil},
		{"PATCH", "/v1/phases/{id}", "Fases", "Editar/cerrar fase", "PhaseUpdate", "Phase", nil},

		{"GET", "/v1/projects/{id}/epics", "Épicas", "Listar épicas", "", "EpicList", []string{"phase_id"}},
		{"POST", "/v1/projects/{id}/epics", "Épicas", "Crear épica", "EpicCreate", "Epic", nil},
		{"GET", "/v1/epics/{id}", "Épicas", "Obtener épica con tickets", "", "Epic", nil},
		{"PATCH", "/v1/epics/{id}", "Épicas", "Editar/mover/cerrar épica", "EpicUpdate", "Epic", nil},

		{"POST", "/v1/messages", "Mensajes", "Enviar mensaje info o solicitud", "MessageCreate", "Message", nil},
		{"GET", "/v1/messages", "Mensajes", "Listar mensajes enviados", "", "MessageList", []string{"project_id", "limit", "cursor"}},
		{"GET", "/v1/messages/{id}", "Mensajes", "Obtener mensaje", "", "Message", nil},
		{"GET", "/v1/me/bandeja", "Mensajes", "Bandeja del participante", "", "MessageList", []string{"pendientes", "project_id", "limit", "cursor"}},
		{"POST", "/v1/messages/{id}/estado", "Mensajes", "Cambiar estado visto/atendido", "MessageState", "Message", nil},
		{"POST", "/v1/messages/{id}/visto", "Mensajes", "Marcar visto", "", "Message", nil},
		{"POST", "/v1/messages/{id}/acusar", "Mensajes", "Marcar atendido", "", "Message", nil},
		{"POST", "/v1/messages/{id}/tomar", "Mensajes", "Tomar mensaje de rol", "", "Message", nil},
		{"POST", "/v1/messages/{id}/resolver", "Mensajes", "Resolver solicitud", "MessageResolve", "Message", nil},
		{"POST", "/v1/messages/{id}/responder", "Mensajes", "Alias de resolver", "MessageResolve", "Message", nil},
		{"POST", "/v1/messages/{id}/descartar", "Mensajes", "Descartar solicitud (emisor)", "", "Message", nil},
		{"POST", "/v1/messages/{id}/convertir", "Mensajes", "Convertir a decisión o ticket", "MessageConvert", "FreeObject", nil},
		{"POST", "/v1/messages/{id}/handoff/aceptar", "Mensajes", "Aceptar handoff", "", "Message", nil},
		{"POST", "/v1/messages/{id}/handoff/rechazar", "Mensajes", "Rechazar handoff", "", "Message", nil},

		{"GET", "/v1/projects/{id}/documents", "Documentos", "Listar docs de proyecto + globales", "", "DocumentList", []string{"limit", "cursor"}},
		{"POST", "/v1/projects/{id}/documents", "Documentos", "Crear documento de proyecto", "DocumentCreate", "Document", nil},
		{"POST", "/v1/projects/{id}/documents/sync", "Documentos", "Sincronizar docs de proyecto", "DocumentSync", "SyncResult", nil},
		{"GET", "/v1/documents", "Documentos", "Listar docs globales", "", "DocumentList", []string{"limit", "cursor"}},
		{"POST", "/v1/documents", "Documentos", "Crear documento global (admin)", "DocumentCreate", "Document", nil},
		{"POST", "/v1/documents/sync", "Documentos", "Sincronizar docs globales (admin)", "DocumentSync", "SyncResult", nil},
		{"GET", "/v1/documents/{id}", "Documentos", "Obtener documento/versión", "", "Document", []string{"version"}},
		{"POST", "/v1/documents/{id}/versions", "Documentos", "Crear versión", "DocumentVersion", "Document", nil},
		{"GET", "/v1/documents/{id}/versions", "Documentos", "Listar versiones", "", "DocumentVersionList", nil},
		{"GET", "/v1/documents/{id}/diff", "Documentos", "Diff entre versiones", "", "FreeObject", []string{"from", "to"}},
		{"GET", "/v1/projects/{id}/decisions", "Documentos", "Listar decisiones/ADRs", "", "DecisionList", nil},
		{"POST", "/v1/projects/{id}/decisions", "Documentos", "Crear decisión/ADR", "DecisionCreate", "FreeObject", nil},

		{"GET", "/v1/search", "Búsqueda", "Buscar tickets/docs/mensajes", "", "SearchList", []string{"q", "type", "project_id", "limit"}},
		{"GET", "/v1/projects/{id}/briefing", "Búsqueda", "Briefing de arranque", "", "FreeObject", nil},
		{"GET", "/v1/projects/{id}/digest", "Búsqueda", "Digest del proyecto", "", "FreeObject", nil},
		{"GET", "/v1/projects/{id}/tablero", "Búsqueda", "Tablero fases→épicas→tickets", "", "FreeObject", nil},
		{"GET", "/v1/novedades", "Búsqueda", "Long-poll de novedades", "", "Novedades", []string{"since", "wait"}},

		{"GET", "/v1/audit/chains", "Auditoría", "Listar cadenas", "", "ChainList", nil},
		{"GET", "/v1/audit", "Auditoría", "Consultar eventos", "", "AuditList", []string{"chain_id", "actor_id", "action", "object_type", "object_id", "project_id", "from", "to", "limit", "cursor"}},
		{"GET", "/v1/audit/verify", "Auditoría", "Verificar una cadena", "", "VerifyResult", []string{"chain_id"}},
		{"POST", "/v1/admin/verify-all", "Auditoría", "Verificar todas las cadenas", "", "FreeObject", nil},

		{"POST", "/v1/projects/{id}/import", "Operación", "Importar fases/épicas/tickets", "ImportRequest", "ImportResult", []string{"dry_run", "modo"}},
		{"POST", "/v1/admin/barridos", "Operación", "Liberar claims y asumir defaults", "", "FreeObject", nil},
		{"POST", "/v1/admin/claims/sin-actividad", "Operación", "Alias de barridos", "", "FreeObject", nil},
		{"GET", "/v1/admin/metrics", "Operación", "Métricas", "", "Metrics", nil},
		{"GET", "/v1/admin/outbox", "Operación", "Listar outbox", "", "OutboxList", []string{"status", "limit"}},
		{"POST", "/v1/admin/outbox/despachar", "Operación", "Despachar outbox", "", "FreeObject", nil},
		{"GET", "/v1/projects/{id}/webhooks", "Operación", "Listar webhooks", "", "WebhookList", nil},
		{"POST", "/v1/projects/{id}/webhooks", "Operación", "Crear webhook", "WebhookCreate", "Webhook", nil},
		{"DELETE", "/v1/webhooks/{id}", "Operación", "Eliminar webhook", "", "FreeObject", nil},
	}
}

func operationID(method, path string) string {
	clean := strings.TrimPrefix(path, "/")
	clean = strings.NewReplacer("/", "_", "{", "", "}", "", "-", "_", ".", "_").Replace(clean)
	return strings.ToLower(method) + "_" + clean
}

func pathParameters(path string) []any {
	params := make([]any, 0, 2)
	for {
		start := strings.Index(path, "{")
		if start < 0 {
			break
		}
		end := strings.Index(path[start:], "}")
		if end < 0 {
			break
		}
		name := path[start+1 : start+end]
		params = append(params, map[string]any{
			"name": name, "in": "path", "required": true,
			"schema": map[string]any{"type": "string"},
		})
		path = path[start+end+1:]
	}
	return params
}

func jsonContent(schema string) map[string]any {
	return map[string]any{
		"application/json": map[string]any{
			"schema": map[string]any{"$ref": "#/components/schemas/" + schema},
		},
	}
}

func errorResponse(description string) map[string]any {
	return map[string]any{"description": description, "content": jsonContent("Error")}
}

func openAPISchemas() map[string]any {
	str := func() map[string]any { return map[string]any{"type": "string"} }
	intt := func() map[string]any { return map[string]any{"type": "integer"} }
	boolean := func() map[string]any { return map[string]any{"type": "boolean"} }
	date := func() map[string]any { return map[string]any{"type": "string", "format": "date-time"} }
	arr := func(schema string) map[string]any {
		return map[string]any{"type": "array", "items": map[string]any{"$ref": "#/components/schemas/" + schema}}
	}
	obj := func(props map[string]any, required ...string) map[string]any {
		schema := map[string]any{"type": "object", "additionalProperties": true, "properties": props}
		if len(required) > 0 {
			schema["required"] = required
		}
		return schema
	}
	ref := func(name string) map[string]any { return map[string]any{"$ref": "#/components/schemas/" + name} }
	free := map[string]any{"type": "object", "additionalProperties": true}
	list := func(name string) map[string]any {
		return map[string]any{
			"type": "object",
			"properties": map[string]any{
				"items":       arr(name),
				"next_cursor": map[string]any{"type": "string", "nullable": true},
			},
		}
	}
	schemas := map[string]any{
		"Error":               obj(map[string]any{"error": str(), "code": str()}, "error", "code"),
		"FreeObject":          free,
		"Health":              obj(map[string]any{"ok": boolean(), "db": str()}),
		"Meta":                obj(map[string]any{"name": str(), "version": str(), "api_version": str(), "database": str(), "features": map[string]any{"type": "array", "items": str()}}),
		"Project":             obj(map[string]any{"id": str(), "slug": str(), "name": str(), "prefix": str(), "version": intt(), "permission": str(), "roles": map[string]any{"type": "array", "items": str()}, "created_at": date(), "updated_at": date()}),
		"ProjectList":         list("Project"),
		"ProjectCreate":       obj(map[string]any{"slug": str(), "name": str(), "prefix": str()}, "slug", "name"),
		"ProjectUpdate":       obj(map[string]any{"version": intt(), "slug": str(), "name": str(), "prefix": str()}),
		"Participant":         obj(map[string]any{"id": str(), "type": str(), "name": str(), "status": str(), "admin": boolean(), "created_at": date()}),
		"ParticipantList":     list("Participant"),
		"ParticipantCreate":   obj(map[string]any{"name": str(), "type": str(), "admin": boolean(), "projects": map[string]any{"type": "array", "items": free}}, "name", "type"),
		"ParticipantCreated":  obj(map[string]any{"participant": ref("Participant"), "token": str(), "token_id": str()}),
		"TokenCreated":        obj(map[string]any{"token": str(), "token_id": str()}),
		"Member":              obj(map[string]any{"participant_id": str(), "name": str(), "type": str(), "admin": boolean(), "permission": str(), "roles": map[string]any{"type": "array", "items": str()}, "created_at": date()}),
		"MemberList":          list("Member"),
		"MembershipUpdate":    obj(map[string]any{"permission": str(), "roles": map[string]any{"type": "array", "items": str()}}, "permission"),
		"Role":                obj(map[string]any{"name": str(), "members": intt(), "created_at": date()}),
		"RoleList":            list("Role"),
		"Content":             obj(map[string]any{"content": str(), "untrusted": boolean(), "source": str()}),
		"Claim":               obj(map[string]any{"id": str(), "participant_id": str(), "name": str(), "lease_until": date(), "created_at": date()}),
		"ClaimResult":         obj(map[string]any{"ticket": ref("Ticket"), "claim": ref("Claim")}),
		"Dependency":          obj(map[string]any{"id": str(), "project_id": str(), "ref": str(), "title": str(), "status": str()}),
		"Ticket":              obj(map[string]any{"id": str(), "project_id": str(), "ref": str(), "number": intt(), "key": str(), "title": str(), "description": str(), "description_content": ref("Content"), "status": str(), "effective_status": str(), "priority": str(), "assignee_id": str(), "role": str(), "phase_id": str(), "epic_id": str(), "order_index": intt(), "version": intt(), "paths": map[string]any{"type": "array", "items": str()}, "discovered_in": str(), "created_by": str(), "created_at": date(), "updated_at": date(), "dependencies": arr("Dependency"), "dependents": arr("Dependency"), "claims": arr("Claim"), "comments_count": intt()}),
		"TicketList":          list("Ticket"),
		"TicketCreate":        obj(map[string]any{"title": str(), "description": str(), "status": str(), "priority": str(), "assignee_id": str(), "role": str(), "phase_id": str(), "epic_id": str(), "order_index": intt(), "paths": map[string]any{"type": "array", "items": str()}, "discovered_in": str(), "key": str(), "depends_on": map[string]any{"type": "array", "items": str()}}, "title"),
		"TicketUpdate":        obj(map[string]any{"version": intt(), "title": str(), "description": str(), "status": str(), "priority": str(), "assignee_id": str(), "role": str(), "phase_id": str(), "epic_id": str(), "order_index": intt(), "paths": map[string]any{"type": "array", "items": str()}, "discovered_in": str()}),
		"TicketAdvance":       obj(map[string]any{"version": intt(), "status": str(), "comment": str(), "nota_cierre": str(), "pregunta": free, "y_siguiente": boolean()}),
		"AdvanceResult":       obj(map[string]any{"ticket": ref("Ticket"), "message": ref("Message"), "next": ref("TicketNextResult")}),
		"Comment":             obj(map[string]any{"id": str(), "ticket_id": str(), "author_id": str(), "author_name": str(), "type": str(), "body": str(), "body_content": ref("Content"), "created_at": date()}),
		"CommentList":         list("Comment"),
		"CommentCreate":       obj(map[string]any{"body": str(), "type": str()}, "body"),
		"TimelineItem":        obj(map[string]any{"id": str(), "type": str(), "author_id": str(), "author_name": str(), "action": str(), "body": str(), "body_content": ref("Content"), "payload": free, "created_at": date()}),
		"TimelineList":        list("TimelineItem"),
		"DependencyCreate":    obj(map[string]any{"depends_on": str()}, "depends_on"),
		"TicketNext":          obj(map[string]any{"project_id": str(), "role": str(), "phase_id": str(), "epic_id": str(), "claim": boolean()}),
		"TicketNextResult":    obj(map[string]any{"ticket": ref("Ticket"), "candidates": arr("Ticket"), "motivo": str(), "motivos": map[string]any{"type": "object", "additionalProperties": intt()}, "claims_activos": intt(), "contexto": free}),
		"Phase":               obj(map[string]any{"id": str(), "project_id": str(), "key": str(), "name": str(), "description": str(), "order_index": intt(), "status": str(), "created_at": date(), "closed_at": date(), "progress": free, "epics": arr("Epic"), "tickets": arr("Ticket")}),
		"PhaseList":           list("Phase"),
		"PhaseCreate":         obj(map[string]any{"key": str(), "name": str(), "description": str(), "order_index": intt()}, "name"),
		"PhaseUpdate":         obj(map[string]any{"key": str(), "name": str(), "description": str(), "order_index": intt(), "status": str()}),
		"Epic":                obj(map[string]any{"id": str(), "project_id": str(), "phase_id": str(), "key": str(), "title": str(), "description": str(), "order_index": intt(), "status": str(), "created_at": date(), "closed_at": date(), "tickets": arr("Ticket")}),
		"EpicList":            list("Epic"),
		"EpicCreate":          obj(map[string]any{"key": str(), "title": str(), "description": str(), "phase_id": str(), "order_index": intt()}, "title"),
		"EpicUpdate":          obj(map[string]any{"key": str(), "title": str(), "description": str(), "phase_id": str(), "order_index": intt(), "status": str()}),
		"Message":             obj(map[string]any{"id": str(), "project_id": str(), "from_participant_id": str(), "from_name": str(), "type": str(), "destination_type": str(), "destination_participant_id": str(), "destination_role": str(), "subject": str(), "body": str(), "body_content": ref("Content"), "options": map[string]any{"type": "array", "items": str()}, "default_option": intt(), "assume_default_at": date(), "resolution": str(), "resolved_by": str(), "resolved_at": date(), "response_option": intt(), "response_text": str(), "taken_by": str(), "taken_at": date(), "reply_to": str(), "correlation_id": str(), "state": str(), "pending": boolean(), "affects": map[string]any{"type": "array", "items": str()}, "refs": map[string]any{"type": "array", "items": free}, "created_at": date()}),
		"MessageList":         list("Message"),
		"MessageCreate":       obj(map[string]any{"project_id": str(), "type": str(), "destination_type": str(), "destination_participant_id": str(), "destination_role": str(), "subject": str(), "body": str(), "options": map[string]any{"type": "array", "items": str()}, "default_option": intt(), "assume_default_at": str(), "affects": map[string]any{"type": "array", "items": str()}, "refs": map[string]any{"type": "array", "items": free}, "reply_to": str()}, "subject", "body"),
		"MessageState":        obj(map[string]any{"state": str()}, "state"),
		"MessageResolve":      obj(map[string]any{"option": intt(), "text": str()}),
		"MessageConvert":      obj(map[string]any{"to": str(), "title": str(), "body": str()}, "to"),
		"Document":            obj(map[string]any{"id": str(), "project_id": str(), "title": str(), "route": str(), "type": str(), "pinned": boolean(), "current_version": intt(), "version": intt(), "hash": str(), "content": str(), "content_content": ref("Content"), "author_id": str(), "author_name": str(), "created_at": date(), "updated_at": date()}),
		"DocumentList":        list("Document"),
		"DocumentCreate":      obj(map[string]any{"title": str(), "route": str(), "type": str(), "pinned": boolean(), "content": str()}, "title", "content"),
		"DocumentVersion":     obj(map[string]any{"content": str()}, "content"),
		"DocumentVersionList": list("Document"),
		"DocumentSync":        obj(map[string]any{"documents": map[string]any{"type": "array", "items": free}}, "documents"),
		"SyncResult":          obj(map[string]any{"items": map[string]any{"type": "array", "items": free}}),
		"DecisionCreate":      obj(map[string]any{"title": str(), "body": str(), "source_message_id": str()}, "title", "body"),
		"DecisionList":        list("FreeObject"),
		"SearchList":          list("FreeObject"),
		"AuditEvent":          obj(map[string]any{"id": str(), "chain_id": str(), "seq": intt(), "actor_type": str(), "actor_id": str(), "action": str(), "object_type": str(), "object_id": str(), "project_id": str(), "session_id": str(), "origin": str(), "correlation_id": str(), "parent_event_id": str(), "payload": free, "prev_hash": str(), "hash": str(), "created_at": date()}),
		"AuditList":           list("AuditEvent"),
		"ChainList":           list("FreeObject"),
		"VerifyResult":        obj(map[string]any{"ok": boolean(), "chain_id": str(), "events": intt(), "first_error": free}),
		"Novedades":           obj(map[string]any{"items": arr("AuditEvent"), "next_cursor": str(), "waited": boolean()}),
		"ImportRequest":       obj(map[string]any{"fases": map[string]any{"type": "array", "items": free}, "epicas": map[string]any{"type": "array", "items": free}, "tickets": map[string]any{"type": "array", "items": free}}),
		"ImportResult":        obj(map[string]any{"phases_created": intt(), "epics_created": intt(), "tickets_created": intt(), "tickets_updated": intt(), "tickets_skipped": intt(), "tickets_total": intt(), "dry_run": boolean()}),
		"Webhook":             obj(map[string]any{"id": str(), "project_id": str(), "url": str(), "events": map[string]any{"type": "array", "items": str()}, "active": boolean(), "created_at": date()}),
		"WebhookList":         list("Webhook"),
		"WebhookCreate":       obj(map[string]any{"url": str(), "secret": str(), "events": map[string]any{"type": "array", "items": str()}}, "url"),
		"OutboxItem":          obj(map[string]any{"id": str(), "kind": str(), "destination": str(), "payload": free, "attempts": intt(), "next_attempt_at": date(), "status": str(), "last_error": str(), "created_at": date(), "updated_at": date()}),
		"OutboxList":          list("OutboxItem"),
		"Metrics":             obj(map[string]any{"counts": map[string]any{"type": "object", "additionalProperties": intt()}, "active_claims": intt(), "pending_outbox": intt()}),
		"Me":                  obj(map[string]any{"participant": ref("Participant"), "memberships": map[string]any{"type": "array", "items": free}, "session_id": str(), "briefing": free}),
	}
	return schemas
}

func (s *Server) openapi(w http.ResponseWriter, _ *http.Request) {
	s.writeJSON(w, http.StatusOK, openAPIDocument())
}

func (s *Server) swagger(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(`<!doctype html>
<html lang="es">
<head>
  <meta charset="utf-8">
  <title>AgentBank Go — Swagger</title>
  <link rel="stylesheet" href="https://unpkg.com/swagger-ui-dist@5/swagger-ui.css">
</head>
<body>
  <div id="swagger-ui"></div>
  <script src="https://unpkg.com/swagger-ui-dist@5/swagger-ui-bundle.js"></script>
  <script>
    SwaggerUIBundle({url: '/openapi.json', dom_id: '#swagger-ui', deepLinking: true});
  </script>
</body>
</html>`))
}
