// Package agentruntime resuelve la identidad de un agente y la enrola si hace
// falta, usando el token humano solo para administración.
package agentruntime

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/joacomedel/agentbank-go/internal/client"
	"github.com/joacomedel/agentbank-go/internal/credentials"
)

// Options describe el agente a asegurar.
type Options struct {
	BaseURL    string
	HumanToken string
	Project    string
	Agent      string
	Role       string
}

// Result informa qué se hizo.
type Result struct {
	Agent    string `json:"agent"`
	Token    string `json:"-"`
	Created  bool   `json:"created"`
	Rotated  bool   `json:"rotated"`
	Enrolled bool   `json:"enrolled"`
}

// Ensure devuelve un token de agente válido. Si no existe, lo crea/reutiliza
// usando el token humano y guarda el token local.
func Ensure(ctx context.Context, opts Options) (Result, error) {
	agent := strings.TrimSpace(opts.Agent)
	if agent == "" {
		return Result{}, errors.New("falta AGENTBANK_AGENT o --agent")
	}
	agent = credentials.ResolveAgentName(agent, opts.Role)
	tokenPath := credentials.TokenPath(agent)
	if token := credentials.ReadToken(tokenPath); token != "" {
		probe := client.New(opts.BaseURL, token, opts.Project)
		if _, err := probe.Do(ctx, "GET", "/v1/me", nil, nil, nil); err == nil {
			return Result{Agent: agent, Token: token}, nil
		}
	}
	if strings.TrimSpace(opts.HumanToken) == "" {
		return Result{}, fmt.Errorf("el agente %q no tiene token válido y no hay token humano; corré `agentbank login`", agent)
	}
	human := client.New(opts.BaseURL, opts.HumanToken, opts.Project)
	existing, err := findAgent(ctx, human, agent)
	if err != nil {
		return Result{}, err
	}
	result := Result{Agent: agent, Enrolled: true}
	var participantID string
	if existing != nil {
		participantID, _ = existing["id"].(string)
		if participantID == "" {
			return Result{}, fmt.Errorf("participante %q sin id", agent)
		}
		if status, _ := existing["status"].(string); status == "baja" {
			return Result{}, fmt.Errorf("el agente %q está de baja; reactivalo o usá otro nombre", agent)
		}
		if role := strings.TrimSpace(opts.Role); role != "" {
			if _, err := human.Do(ctx, "PATCH", "/v1/participants/"+url.PathEscape(participantID), nil,
				map[string]any{"agent_role": role}, nil); err != nil {
				return Result{}, fmt.Errorf("actualizar rol del agente: %w", err)
			}
		}
		if strings.TrimSpace(opts.Project) != "" {
			if err := ensureMembership(ctx, human, opts.Project, participantID, opts.Role); err != nil {
				return Result{}, err
			}
		}
		response, err := human.Do(ctx, "POST", "/v1/participants/"+url.PathEscape(participantID)+"/tokens", nil, nil, nil)
		if err != nil {
			return Result{}, fmt.Errorf("rotar token del agente: %w", err)
		}
		token := stringField(response, "token")
		if token == "" {
			return Result{}, fmt.Errorf("la rotación no devolvió token")
		}
		result.Token = token
		result.Rotated = true
	} else {
		if strings.TrimSpace(opts.Project) == "" {
			return Result{}, errors.New("falta AGENTBANK_PROJECT para enrolar un agente nuevo")
		}
		body := map[string]any{
			"name":       agent,
			"type":       "agente",
			"agent_role": strings.TrimSpace(opts.Role),
			"projects": []map[string]any{{
				"project_id": opts.Project,
				"permission": "colaborador",
				"roles":      roleList(opts.Role),
			}},
		}
		response, err := human.Do(ctx, "POST", "/v1/participants", nil, body, nil)
		if err != nil {
			return Result{}, fmt.Errorf("crear agente %q: %w", agent, err)
		}
		token := stringField(response, "token")
		if token == "" {
			return Result{}, fmt.Errorf("el alta del agente no devolvió token")
		}
		result.Token = token
		result.Created = true
	}
	if err := credentials.WriteToken(tokenPath, result.Token); err != nil {
		return Result{}, err
	}
	return result, nil
}

func findAgent(ctx context.Context, human *client.Client, name string) (map[string]any, error) {
	query := url.Values{}
	query.Set("type", "agente")
	query.Set("name", name)
	query.Set("limit", "20")
	response, err := human.Do(ctx, "GET", "/v1/participants", query, nil, nil)
	if err != nil {
		return nil, fmt.Errorf("buscar agente: %w", err)
	}
	for _, item := range itemsOf(response) {
		if stringField(item, "name") == name {
			return item, nil
		}
	}
	return nil, nil
}

func ensureMembership(ctx context.Context, human *client.Client, project, participantID, role string) error {
	query := url.Values{}
	query.Set("limit", "200")
	response, err := human.Do(ctx, "GET", "/v1/projects/"+url.PathEscape(project)+"/members", query, nil, nil)
	if err != nil {
		return fmt.Errorf("listar miembros: %w", err)
	}
	for _, item := range itemsOf(response) {
		if stringField(item, "participant_id") == participantID {
			return nil
		}
	}
	body := map[string]any{
		"permission": "colaborador",
		"roles":      roleList(role),
	}
	if _, err := human.Do(ctx, "PUT", "/v1/projects/"+url.PathEscape(project)+"/members/"+url.PathEscape(participantID), nil, body, nil); err != nil {
		return fmt.Errorf("crear membresía del agente: %w", err)
	}
	return nil
}

func itemsOf(response any) []map[string]any {
	root, _ := response.(map[string]any)
	raw, _ := root["items"].([]any)
	items := make([]map[string]any, 0, len(raw))
	for _, item := range raw {
		if mapped, ok := item.(map[string]any); ok {
			items = append(items, mapped)
		}
	}
	return items
}

func stringField(root any, key string) string {
	mapped, _ := root.(map[string]any)
	if mapped == nil {
		return ""
	}
	value, _ := mapped[key].(string)
	return value
}

func roleList(role string) []string {
	role = strings.TrimSpace(role)
	if role == "" {
		return []string{}
	}
	return []string{role}
}
