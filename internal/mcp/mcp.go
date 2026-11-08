// Package mcp implementa un servidor MCP por stdio sobre la API HTTP.
package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"regexp"
	"strings"

	"github.com/joacomedel/agentbank-go/internal/client"
)

type tool struct {
	Name        string
	Description string
	Method      string
	Path        string
	Query       []string
	Body        []string
	Properties  map[string]any
	Required    []string
}

var pathParamPattern = regexp.MustCompile(`\{([a-z_]+)\}`)

// Run ejecuta el loop MCP leyendo JSON-RPC por líneas.
func Run(ctx context.Context, api *client.Client, in io.Reader, out, errOut io.Writer, version string) error {
	scanner := bufio.NewScanner(in)
	scanner.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	encoder := json.NewEncoder(out)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var request jsonRPCRequest
		if err := json.Unmarshal([]byte(line), &request); err != nil {
			_ = encoder.Encode(errorResponse(nil, -32700, "JSON inválido"))
			continue
		}
		if request.ID == nil {
			// Notificación: no se responde.
			continue
		}
		response := handleRequest(ctx, api, request, version)
		if err := encoder.Encode(response); err != nil {
			return err
		}
	}
	return scanner.Err()
}

type jsonRPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

type jsonRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func handleRequest(ctx context.Context, api *client.Client, request jsonRPCRequest, version string) jsonRPCResponse {
	switch request.Method {
	case "initialize":
		return successResponse(request.ID, map[string]any{
			"protocolVersion": "2025-06-18",
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo":      map[string]any{"name": "agentbank-go", "version": version},
			"instructions":    "Usá tools/list. No hay perfiles: todas las tools están disponibles. Preferí changes/updated_since y ticket_siguiente para minimizar llamadas.",
		})
	case "ping":
		return successResponse(request.ID, map[string]any{})
	case "tools/list":
		return successResponse(request.ID, map[string]any{"tools": toolList()})
	case "tools/call":
		var params struct {
			Name      string         `json:"name"`
			Arguments map[string]any `json:"arguments"`
		}
		if err := json.Unmarshal(request.Params, &params); err != nil {
			return errorResponse(request.ID, -32602, "params inválidos")
		}
		result, err := callTool(ctx, api, params.Name, params.Arguments)
		if err != nil {
			return successResponse(request.ID, map[string]any{
				"content": []map[string]any{{"type": "text", "text": err.Error()}},
				"isError": true,
			})
		}
		encoded, _ := json.MarshalIndent(result, "", "  ")
		return successResponse(request.ID, map[string]any{
			"content": []map[string]any{{"type": "text", "text": string(encoded)}},
			"isError": false,
		})
	default:
		return errorResponse(request.ID, -32601, "método no encontrado: "+request.Method)
	}
}

func successResponse(id json.RawMessage, result any) jsonRPCResponse {
	return jsonRPCResponse{JSONRPC: "2.0", ID: id, Result: result}
}

func errorResponse(id json.RawMessage, code int, message string) jsonRPCResponse {
	return jsonRPCResponse{JSONRPC: "2.0", ID: id, Error: &rpcError{Code: code, Message: message}}
}

func callTool(ctx context.Context, api *client.Client, name string, args map[string]any) (any, error) {
	if name == "api_request" {
		method, _ := args["method"].(string)
		path, _ := args["path"].(string)
		if method == "" || path == "" {
			return nil, fmt.Errorf("api_request requiere method y path")
		}
		query := url.Values{}
		if rawQuery, ok := args["query"].(map[string]any); ok {
			for key, value := range rawQuery {
				query.Set(key, fmt.Sprint(value))
			}
		}
		headers := map[string]string{}
		if key, _ := args["idempotency_key"].(string); key != "" {
			headers["Idempotency-Key"] = key
		}
		return api.Do(ctx, strings.ToUpper(method), path, query, args["body"], headers)
	}
	definition, ok := toolByName(name)
	if !ok {
		return nil, fmt.Errorf("tool desconocida: %s", name)
	}
	path := definition.Path
	for _, match := range pathParamPattern.FindAllStringSubmatch(definition.Path, -1) {
		param := match[1]
		value := strings.TrimSpace(fmt.Sprint(args[param]))
		if value == "" || value == "<nil>" {
			if param == "id" && api.Project != "" && strings.HasPrefix(definition.Path, "/v1/projects/{id}") {
				value = api.Project
			} else if param == "project_id" && api.Project != "" {
				value = api.Project
			} else {
				return nil, fmt.Errorf("falta argumento de path %q", param)
			}
		}
		path = strings.ReplaceAll(path, "{"+param+"}", url.PathEscape(value))
	}
	query := url.Values{}
	for _, key := range definition.Query {
		if value, ok := args[key]; ok && value != nil && fmt.Sprint(value) != "" {
			query.Set(key, fmt.Sprint(value))
		}
	}
	body := map[string]any{}
	bodyKeys := definition.Body
	if len(bodyKeys) == 0 && definition.Method != "GET" && definition.Method != "DELETE" {
		excluded := map[string]bool{"idempotency_key": true}
		for _, match := range pathParamPattern.FindAllStringSubmatch(definition.Path, -1) {
			excluded[match[1]] = true
		}
		for _, key := range definition.Query {
			excluded[key] = true
		}
		for key, value := range args {
			if !excluded[key] {
				body[key] = value
			}
		}
	} else {
		for _, key := range bodyKeys {
			if value, ok := args[key]; ok {
				body[key] = value
			}
		}
	}
	var payload any
	if definition.Method != "GET" && definition.Method != "DELETE" && len(body) > 0 {
		payload = body
	}
	headers := map[string]string{}
	if key, _ := args["idempotency_key"].(string); key != "" {
		headers["Idempotency-Key"] = key
	}
	return api.Do(ctx, definition.Method, path, query, payload, headers)
}

func toolList() []map[string]any {
	tools := tools()
	out := make([]map[string]any, 0, len(tools))
	for _, definition := range tools {
		properties := definition.Properties
		if properties == nil {
			properties = map[string]any{}
		}
		required := append([]string{}, definition.Required...)
		for _, match := range pathParamPattern.FindAllStringSubmatch(definition.Path, -1) {
			param := match[1]
			found := false
			for _, item := range required {
				if item == param {
					found = true
				}
			}
			if !found {
				required = append(required, param)
			}
		}
		out = append(out, map[string]any{
			"name":        definition.Name,
			"description": definition.Description,
			"inputSchema": map[string]any{
				"type":       "object",
				"properties": properties,
				"required":   required,
			},
		})
	}
	return out
}

func toolByName(name string) (tool, bool) {
	for _, definition := range tools() {
		if definition.Name == name {
			return definition, true
		}
	}
	return tool{}, false
}

func prop(kind, description string) map[string]any {
	return map[string]any{"type": kind, "description": description}
}

func arrProp(description string) map[string]any {
	return map[string]any{"type": "array", "description": description, "items": map[string]any{"type": "string"}}
}

func objProp(description string) map[string]any {
	return map[string]any{"type": "object", "description": description, "additionalProperties": true}
}

func arrObjProp(description string) map[string]any {
	return map[string]any{"type": "array", "description": description, "items": map[string]any{"type": "object", "additionalProperties": true}}
}

func tools() []tool {
	project := prop("string", "Proyecto por id o slug")
	id := prop("string", "UUID, PREFIX-N o key")
	version := prop("integer", "Versión para control optimista")
	idempotency := prop("string", "Idempotency-Key opcional")
	return []tool{
		{Name: "me", Description: "Identidad, membresías y briefing. 1 llamada.", Method: "GET", Path: "/v1/me", Query: []string{"proyecto"}, Properties: map[string]any{"proyecto": project}},
		{Name: "changes", Description: "Cambios incrementales de tickets/mensajes/docs/fases/épicas desde un cursor. 1 llamada.", Method: "GET", Path: "/v1/changes", Query: []string{"since", "project_id", "limit"}, Properties: map[string]any{"since": prop("string", "Cursor"), "project_id": project, "limit": prop("integer", "Máximo")}},
		{Name: "novedades", Description: "Long-poll de eventos nuevos. 1 llamada.", Method: "GET", Path: "/v1/novedades", Query: []string{"since", "wait"}, Properties: map[string]any{"since": prop("string", "Cursor"), "wait": prop("integer", "Segundos, máx 30")}},
		{Name: "projects_list", Description: "Listar proyectos legibles.", Method: "GET", Path: "/v1/projects", Query: []string{"limit", "cursor"}, Properties: map[string]any{"limit": prop("integer", "Máximo"), "cursor": prop("string", "Cursor")}},
		{Name: "project_create", Description: "Crear proyecto (admin de instancia).", Method: "POST", Path: "/v1/projects", Properties: map[string]any{"slug": prop("string", "Slug"), "name": prop("string", "Nombre"), "prefix": prop("string", "Prefijo"), "idempotency_key": idempotency}, Body: []string{"slug", "name", "prefix"}},
		{Name: "project_get", Description: "Obtener proyecto por id o slug.", Method: "GET", Path: "/v1/projects/{id}", Properties: map[string]any{"id": project}},
		{Name: "project_edit", Description: "Editar metadatos del proyecto.", Method: "PATCH", Path: "/v1/projects/{id}", Properties: map[string]any{"id": project, "version": version, "name": prop("string", "Nombre"), "slug": prop("string", "Slug"), "prefix": prop("string", "Prefijo"), "idempotency_key": idempotency}, Body: []string{"version", "name", "slug", "prefix"}},
		{Name: "project_pause", Description: "Pausar cola del proyecto.", Method: "POST", Path: "/v1/projects/{id}/pausa", Properties: map[string]any{"id": project}},
		{Name: "project_resume", Description: "Reanudar cola del proyecto.", Method: "POST", Path: "/v1/projects/{id}/seguir", Properties: map[string]any{"id": project}},
		{Name: "participants_list", Description: "Listar participantes.", Method: "GET", Path: "/v1/participants", Query: []string{"activos", "limit", "cursor"}, Properties: map[string]any{"activos": prop("string", "1 para activos"), "limit": prop("integer", "Máximo"), "cursor": prop("string", "Cursor")}},
		{Name: "participant_create", Description: "Crear participante y emitir token una vez.", Method: "POST", Path: "/v1/participants", Properties: map[string]any{"name": prop("string", "Nombre"), "type": prop("string", "humano|agente"), "admin": prop("boolean", "Admin de instancia"), "projects": arrObjProp("Membresías"), "idempotency_key": idempotency}, Required: []string{"name", "type"}, Body: []string{"name", "type", "admin", "projects"}},
		{Name: "participant_token", Description: "Rotar token de un participante.", Method: "POST", Path: "/v1/participants/{id}/tokens", Properties: map[string]any{"id": prop("string", "Participante")}},
		{Name: "participant_disable", Description: "Dar de baja participante y revocar tokens.", Method: "POST", Path: "/v1/participants/{id}/baja", Properties: map[string]any{"id": prop("string", "Participante")}},
		{Name: "token_revoke", Description: "Revocar token.", Method: "POST", Path: "/v1/tokens/{id}/revoke", Properties: map[string]any{"id": prop("string", "Token")}},
		{Name: "members_list", Description: "Listar miembros del proyecto.", Method: "GET", Path: "/v1/projects/{id}/members", Query: []string{"limit", "cursor"}, Properties: map[string]any{"id": project, "limit": prop("integer", "Máximo"), "cursor": prop("string", "Cursor")}},
		{Name: "member_set", Description: "Crear/actualizar membresía.", Method: "PUT", Path: "/v1/projects/{id}/members/{participant_id}", Properties: map[string]any{"id": project, "participant_id": prop("string", "Participante"), "permission": prop("string", "lector|colaborador|admin"), "roles": arrProp("Roles"), "idempotency_key": idempotency}, Required: []string{"participant_id", "permission"}, Body: []string{"permission", "roles"}},
		{Name: "member_remove", Description: "Quitar membresía.", Method: "DELETE", Path: "/v1/projects/{id}/members/{participant_id}", Properties: map[string]any{"id": project, "participant_id": prop("string", "Participante")}},
		{Name: "roles_list", Description: "Catálogo de roles del proyecto.", Method: "GET", Path: "/v1/projects/{id}/roles", Query: []string{"limit", "cursor"}, Properties: map[string]any{"id": project, "limit": prop("integer", "Máximo"), "cursor": prop("string", "Cursor")}},
		{Name: "phases_list", Description: "Listar fases.", Method: "GET", Path: "/v1/projects/{id}/phases", Properties: map[string]any{"id": project}},
		{Name: "phase_create", Description: "Crear fase.", Method: "POST", Path: "/v1/projects/{id}/phases", Properties: map[string]any{"id": project, "key": prop("string", "Key"), "name": prop("string", "Nombre"), "description": prop("string", "Descripción"), "order_index": prop("integer", "Orden"), "idempotency_key": idempotency}, Body: []string{"key", "name", "description", "order_index"}},
		{Name: "phase_get", Description: "Obtener fase con épicas y tickets.", Method: "GET", Path: "/v1/phases/{id}", Properties: map[string]any{"id": prop("string", "Fase")}},
		{Name: "phase_edit", Description: "Editar/cerrar fase.", Method: "PATCH", Path: "/v1/phases/{id}", Properties: map[string]any{"id": prop("string", "Fase"), "key": prop("string", "Key"), "name": prop("string", "Nombre"), "description": prop("string", "Descripción"), "order_index": prop("integer", "Orden"), "status": prop("string", "abierta|cerrada"), "idempotency_key": idempotency}, Body: []string{"key", "name", "description", "order_index", "status"}},
		{Name: "epics_list", Description: "Listar épicas.", Method: "GET", Path: "/v1/projects/{id}/epics", Query: []string{"phase_id"}, Properties: map[string]any{"id": project, "phase_id": prop("string", "Fase")}},
		{Name: "epic_create", Description: "Crear épica.", Method: "POST", Path: "/v1/projects/{id}/epics", Properties: map[string]any{"id": project, "key": prop("string", "Key"), "title": prop("string", "Título"), "description": prop("string", "Descripción"), "phase_id": prop("string", "Fase"), "order_index": prop("integer", "Orden"), "idempotency_key": idempotency}, Body: []string{"key", "title", "description", "phase_id", "order_index"}},
		{Name: "epic_get", Description: "Obtener épica con tickets.", Method: "GET", Path: "/v1/epics/{id}", Properties: map[string]any{"id": prop("string", "Épica")}},
		{Name: "epic_edit", Description: "Editar/mover/cerrar épica.", Method: "PATCH", Path: "/v1/epics/{id}", Properties: map[string]any{"id": prop("string", "Épica"), "key": prop("string", "Key"), "title": prop("string", "Título"), "description": prop("string", "Descripción"), "phase_id": prop("string", "Fase"), "order_index": prop("integer", "Orden"), "status": prop("string", "abierta|cerrada"), "idempotency_key": idempotency}, Body: []string{"key", "title", "description", "phase_id", "order_index", "status"}},
		{Name: "tickets_list", Description: "Listar tickets; usar updated_since para solo cambios.", Method: "GET", Path: "/v1/projects/{id}/tickets", Query: []string{"limit", "cursor", "status", "priority", "role", "assignee_id", "phase_id", "epic_id", "key", "updated_since"}, Properties: map[string]any{"id": project, "limit": prop("integer", "Máximo"), "cursor": prop("string", "Cursor"), "status": prop("string", "Estado"), "priority": prop("string", "Prioridad"), "role": prop("string", "Rol"), "assignee_id": prop("string", "Asignado"), "phase_id": prop("string", "Fase"), "epic_id": prop("string", "Épica"), "key": prop("string", "Key"), "updated_since": prop("string", "Solo cambios desde RFC3339")}},
		{Name: "ticket_create", Description: "Crear ticket.", Method: "POST", Path: "/v1/projects/{id}/tickets", Properties: map[string]any{"id": project, "title": prop("string", "Título"), "description": prop("string", "Descripción"), "status": prop("string", "Estado"), "priority": prop("string", "baja|media|alta|urgente"), "assignee_id": prop("string", "Participante"), "role": prop("string", "Rol"), "phase_id": prop("string", "Fase"), "epic_id": prop("string", "Épica"), "order_index": prop("integer", "Orden"), "paths": arrProp("Paths"), "discovered_in": prop("string", "Ticket de origen"), "key": prop("string", "Key"), "depends_on": arrProp("Dependencias"), "idempotency_key": idempotency}, Required: []string{"title"}, Body: []string{"title", "description", "status", "priority", "assignee_id", "role", "phase_id", "epic_id", "order_index", "paths", "discovered_in", "key", "depends_on"}},
		{Name: "ticket_get", Description: "Obtener ticket con dependencias, claims y comentarios.", Method: "GET", Path: "/v1/tickets/{id}", Properties: map[string]any{"id": id}},
		{Name: "ticket_edit", Description: "Editar ticket con versión.", Method: "PATCH", Path: "/v1/tickets/{id}", Properties: map[string]any{"id": id, "version": version, "title": prop("string", "Título"), "description": prop("string", "Descripción"), "status": prop("string", "Estado"), "priority": prop("string", "Prioridad"), "assignee_id": prop("string", "Asignado"), "role": prop("string", "Rol"), "phase_id": prop("string", "Fase"), "epic_id": prop("string", "Épica"), "order_index": prop("integer", "Orden"), "paths": arrProp("Paths"), "discovered_in": prop("string", "Origen"), "idempotency_key": idempotency}, Required: []string{"version"}, Body: []string{"version", "title", "description", "status", "priority", "assignee_id", "role", "phase_id", "epic_id", "order_index", "paths", "discovered_in"}},
		{Name: "ticket_advance", Description: "Avanzar/cerrar ticket con nota y pregunta; puede pedir el siguiente.", Method: "POST", Path: "/v1/tickets/{id}/avanzar", Properties: map[string]any{"id": id, "version": version, "status": prop("string", "Estado"), "comment": prop("string", "Comentario"), "nota_cierre": prop("string", "Nota de cierre"), "pregunta": objProp("Pregunta"), "y_siguiente": prop("boolean", "Reclamar siguiente"), "idempotency_key": idempotency}, Body: []string{"version", "status", "comment", "nota_cierre", "pregunta", "y_siguiente"}},
		{Name: "ticket_next", Description: "Siguiente ticket con contexto y claim. 1 llamada.", Method: "POST", Path: "/v1/tickets/siguiente", Properties: map[string]any{"project_id": project, "role": prop("string", "Rol"), "phase_id": prop("string", "Fase"), "epic_id": prop("string", "Épica"), "claim": prop("boolean", "Reclamar"), "idempotency_key": idempotency}, Body: []string{"project_id", "role", "phase_id", "epic_id", "claim"}},
		{Name: "ticket_claim", Description: "Reclamar ticket.", Method: "POST", Path: "/v1/tickets/{id}/claims", Properties: map[string]any{"id": id}},
		{Name: "ticket_release", Description: "Liberar claim.", Method: "POST", Path: "/v1/tickets/{id}/claims/release", Properties: map[string]any{"id": id}},
		{Name: "ticket_comment", Description: "Comentar ticket.", Method: "POST", Path: "/v1/tickets/{id}/comments", Properties: map[string]any{"id": id, "body": prop("string", "Texto"), "type": prop("string", "comentario|nota|cierre"), "idempotency_key": idempotency}, Required: []string{"body"}, Body: []string{"body", "type"}},
		{Name: "ticket_comments", Description: "Listar comentarios.", Method: "GET", Path: "/v1/tickets/{id}/comments", Query: []string{"limit", "cursor"}, Properties: map[string]any{"id": id, "limit": prop("integer", "Máximo"), "cursor": prop("string", "Cursor")}},
		{Name: "ticket_timeline", Description: "Timeline del ticket.", Method: "GET", Path: "/v1/tickets/{id}/timeline", Query: []string{"limit"}, Properties: map[string]any{"id": id, "limit": prop("integer", "Máximo")}},
		{Name: "ticket_depend", Description: "Agregar dependencia.", Method: "POST", Path: "/v1/tickets/{id}/dependencies", Properties: map[string]any{"id": id, "depends_on": prop("string", "Ticket del que depende"), "idempotency_key": idempotency}, Required: []string{"depends_on"}, Body: []string{"depends_on"}},
		{Name: "ticket_undepend", Description: "Quitar dependencia.", Method: "DELETE", Path: "/v1/tickets/{id}/dependencies/{depends_on}", Properties: map[string]any{"id": id, "depends_on": prop("string", "Ticket del que depende")}},
		{Name: "ticket_documents", Description: "Listar docs del ticket.", Method: "GET", Path: "/v1/tickets/{id}/documents", Properties: map[string]any{"id": id}},
		{Name: "ticket_attach_document", Description: "Adjuntar documento a ticket.", Method: "POST", Path: "/v1/tickets/{id}/documents", Properties: map[string]any{"id": id, "document_id": prop("string", "Documento"), "idempotency_key": idempotency}, Required: []string{"document_id"}, Body: []string{"document_id"}},
		{Name: "messages_send", Description: "Enviar info o solicitud.", Method: "POST", Path: "/v1/messages", Properties: map[string]any{"project_id": project, "type": prop("string", "info|solicitud"), "destination_type": prop("string", "participante|rol|proyecto"), "destination_participant_id": prop("string", "Participante"), "destination_role": prop("string", "Rol"), "subject": prop("string", "Asunto"), "body": prop("string", "Cuerpo"), "options": arrProp("Opciones"), "default_option": prop("integer", "Índice default"), "assume_default_at": prop("string", "RFC3339"), "affects": arrProp("Tickets afectados"), "refs": arrObjProp("Referencias"), "reply_to": prop("string", "Mensaje padre"), "idempotency_key": idempotency}, Required: []string{"subject", "body"}, Body: []string{"project_id", "type", "destination_type", "destination_participant_id", "destination_role", "subject", "body", "options", "default_option", "assume_default_at", "affects", "refs", "reply_to"}},
		{Name: "messages_list", Description: "Listar mensajes enviados.", Method: "GET", Path: "/v1/messages", Query: []string{"project_id", "limit", "cursor"}, Properties: map[string]any{"project_id": project, "limit": prop("integer", "Máximo"), "cursor": prop("string", "Cursor")}},
		{Name: "message_get", Description: "Obtener mensaje.", Method: "GET", Path: "/v1/messages/{id}", Properties: map[string]any{"id": prop("string", "Mensaje")}},
		{Name: "inbox", Description: "Bandeja pendiente. 1 llamada.", Method: "GET", Path: "/v1/me/bandeja", Query: []string{"pendientes", "project_id", "limit", "cursor"}, Properties: map[string]any{"pendientes": prop("string", "1 para pendientes"), "project_id": project, "limit": prop("integer", "Máximo"), "cursor": prop("string", "Cursor")}},
		{Name: "message_state", Description: "Marcar visto/atendido.", Method: "POST", Path: "/v1/messages/{id}/estado", Properties: map[string]any{"id": prop("string", "Mensaje"), "state": prop("string", "visto|atendido")}, Required: []string{"state"}, Body: []string{"state"}},
		{Name: "message_take", Description: "Tomar mensaje de rol.", Method: "POST", Path: "/v1/messages/{id}/tomar", Properties: map[string]any{"id": prop("string", "Mensaje")}},
		{Name: "message_resolve", Description: "Resolver solicitud con opción o texto.", Method: "POST", Path: "/v1/messages/{id}/resolver", Properties: map[string]any{"id": prop("string", "Mensaje"), "option": prop("integer", "Índice"), "text": prop("string", "Texto"), "idempotency_key": idempotency}, Body: []string{"option", "text"}},
		{Name: "message_discard", Description: "Descartar solicitud (emisor).", Method: "POST", Path: "/v1/messages/{id}/descartar", Properties: map[string]any{"id": prop("string", "Mensaje")}},
		{Name: "message_convert", Description: "Convertir a decisión o ticket.", Method: "POST", Path: "/v1/messages/{id}/convertir", Properties: map[string]any{"id": prop("string", "Mensaje"), "to": prop("string", "decision|ticket"), "title": prop("string", "Título"), "body": prop("string", "Cuerpo"), "idempotency_key": idempotency}, Required: []string{"to"}, Body: []string{"to", "title", "body"}},
		{Name: "documents_list", Description: "Listar docs; usar updated_since para solo cambios.", Method: "GET", Path: "/v1/projects/{id}/documents", Query: []string{"limit", "cursor", "updated_since"}, Properties: map[string]any{"id": project, "limit": prop("integer", "Máximo"), "cursor": prop("string", "Cursor"), "updated_since": prop("string", "Solo cambios")}},
		{Name: "document_create", Description: "Crear documento de proyecto.", Method: "POST", Path: "/v1/projects/{id}/documents", Properties: map[string]any{"id": project, "title": prop("string", "Título"), "route": prop("string", "Ruta"), "type": prop("string", "doc|glosario"), "pinned": prop("boolean", "Fijado"), "content": prop("string", "Contenido"), "idempotency_key": idempotency}, Required: []string{"title", "content"}, Body: []string{"title", "route", "type", "pinned", "content"}},
		{Name: "document_get", Description: "Obtener documento/versión.", Method: "GET", Path: "/v1/documents/{id}", Query: []string{"version"}, Properties: map[string]any{"id": prop("string", "Documento"), "version": prop("integer", "Versión")}},
		{Name: "document_version", Description: "Crear versión de documento.", Method: "POST", Path: "/v1/documents/{id}/versions", Properties: map[string]any{"id": prop("string", "Documento"), "content": prop("string", "Contenido"), "idempotency_key": idempotency}, Required: []string{"content"}, Body: []string{"content"}},
		{Name: "document_versions", Description: "Listar versiones.", Method: "GET", Path: "/v1/documents/{id}/versions", Properties: map[string]any{"id": prop("string", "Documento")}},
		{Name: "document_diff", Description: "Diff entre versiones.", Method: "GET", Path: "/v1/documents/{id}/diff", Query: []string{"from", "to"}, Properties: map[string]any{"id": prop("string", "Documento"), "from": prop("integer", "Desde"), "to": prop("integer", "Hasta")}},
		{Name: "documents_sync", Description: "Sincronizar carpeta de docs.", Method: "POST", Path: "/v1/projects/{id}/documents/sync", Properties: map[string]any{"id": project, "documents": arrObjProp("Documentos"), "idempotency_key": idempotency}, Required: []string{"documents"}, Body: []string{"documents"}},
		{Name: "decisions_list", Description: "Listar decisiones/ADRs.", Method: "GET", Path: "/v1/projects/{id}/decisions", Properties: map[string]any{"id": project}},
		{Name: "decision_create", Description: "Crear decisión/ADR.", Method: "POST", Path: "/v1/projects/{id}/decisions", Properties: map[string]any{"id": project, "title": prop("string", "Título"), "body": prop("string", "Cuerpo"), "source_message_id": prop("string", "Mensaje"), "idempotency_key": idempotency}, Required: []string{"title", "body"}, Body: []string{"title", "body", "source_message_id"}},
		{Name: "search", Description: "Buscar tickets/docs/mensajes.", Method: "GET", Path: "/v1/search", Query: []string{"q", "type", "project_id", "limit"}, Properties: map[string]any{"q": prop("string", "Texto"), "type": prop("string", "ticket,document,message"), "project_id": project, "limit": prop("integer", "Máximo")}, Required: []string{"q"}},
		{Name: "briefing", Description: "Briefing de arranque.", Method: "GET", Path: "/v1/projects/{id}/briefing", Properties: map[string]any{"id": project}},
		{Name: "digest", Description: "Digest del proyecto.", Method: "GET", Path: "/v1/projects/{id}/digest", Properties: map[string]any{"id": project}},
		{Name: "tablero", Description: "Tablero fases→épicas→tickets.", Method: "GET", Path: "/v1/projects/{id}/tablero", Properties: map[string]any{"id": project}},
		{Name: "import_plan", Description: "Importar fases/épicas/tickets en una transacción.", Method: "POST", Path: "/v1/projects/{id}/import", Query: []string{"dry_run", "modo"}, Properties: map[string]any{"id": project, "dry_run": prop("string", "1 para dry-run"), "modo": prop("string", "omitir|actualizar"), "fases": arrObjProp("Fases"), "epicas": arrObjProp("Épicas"), "tickets": arrObjProp("Tickets"), "idempotency_key": idempotency}, Body: []string{"fases", "epicas", "tickets"}},
		{Name: "audit_chains", Description: "Listar cadenas de auditoría.", Method: "GET", Path: "/v1/audit/chains"},
		{Name: "audit_list", Description: "Consultar eventos de auditoría.", Method: "GET", Path: "/v1/audit", Query: []string{"chain_id", "actor_id", "action", "object_type", "object_id", "project_id", "from", "to", "limit", "cursor"}, Properties: map[string]any{"chain_id": prop("string", "Cadena"), "actor_id": prop("string", "Actor"), "action": prop("string", "Acción"), "object_type": prop("string", "Tipo"), "object_id": prop("string", "ID"), "project_id": project, "from": prop("string", "Desde"), "to": prop("string", "Hasta"), "limit": prop("integer", "Máximo"), "cursor": prop("string", "Cursor")}},
		{Name: "audit_verify", Description: "Verificar una cadena.", Method: "GET", Path: "/v1/audit/verify", Query: []string{"chain_id"}, Properties: map[string]any{"chain_id": prop("string", "Cadena")}, Required: []string{"chain_id"}},
		{Name: "admin_sweeps", Description: "Liberar claims y asumir defaults vencidos.", Method: "POST", Path: "/v1/admin/barridos"},
		{Name: "admin_metrics", Description: "Métricas operativas.", Method: "GET", Path: "/v1/admin/metrics"},
		{Name: "admin_verify_all", Description: "Verificar todas las cadenas.", Method: "POST", Path: "/v1/admin/verify-all"},
		{Name: "admin_outbox", Description: "Listar outbox.", Method: "GET", Path: "/v1/admin/outbox", Query: []string{"status", "limit"}, Properties: map[string]any{"status": prop("string", "Estado"), "limit": prop("integer", "Máximo")}},
		{Name: "admin_dispatch", Description: "Despachar outbox.", Method: "POST", Path: "/v1/admin/outbox/despachar"},
		{Name: "api_request", Description: "Escape hatch: cualquier método/path de la API.", Method: "GET", Path: "/", Properties: map[string]any{"method": prop("string", "GET|POST|..."), "path": prop("string", "Path /v1/..."), "body": objProp("Body JSON"), "query": objProp("Query params"), "idempotency_key": idempotency}, Required: []string{"method", "path"}},
	}
}
