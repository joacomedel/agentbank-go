// Package cli implementa la CLI de AgentBank Go.
package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/joacomedel/agentbank-go/internal/agentruntime"
	"github.com/joacomedel/agentbank-go/internal/client"
	"github.com/joacomedel/agentbank-go/internal/credentials"
)

// Config es la configuración efectiva de la CLI.
type Config struct {
	URL        string
	Token      string
	HumanToken string
	Project    string
	Agent      string
	AgentRole  string
}

type stringList []string

func (s *stringList) String() string { return strings.Join(*s, ",") }
func (s *stringList) Set(value string) error {
	*s = append(*s, value)
	return nil
}

// Run ejecuta un subcomando de la CLI.
func Run(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		printUsage(stdout)
		return nil
	}
	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	switch args[0] {
	case "help", "-h", "--help":
		printUsage(stdout)
		return nil
	case "login":
		return runLogin(ctx, cfg, args[1:], stdout, stderr)
	case "init":
		return runInit(args[1:], stdout)
	case "agent":
		return runAgent(ctx, cfg, args[1:], stdout, stderr)
	case "worker":
		return runWorker(ctx, cfg, args[1:], stdout, stderr)
	}
	if cfg.Agent != "" && shouldEnsureAgent(args[0]) {
		result, err := agentruntime.Ensure(ctx, agentruntime.Options{
			BaseURL: cfg.URL, HumanToken: cfg.HumanToken, Project: cfg.Project,
			Agent: cfg.Agent, Role: cfg.AgentRole,
		})
		if err != nil {
			return err
		}
		cfg.Token = result.Token
	} else if cfg.Token == "" {
		cfg.Token = cfg.HumanToken
	}
	switch args[0] {
	case "api":
		return runAPI(ctx, cfg, args[1:], stdout, stderr)
	case "me":
		return runMe(ctx, cfg, args[1:], stdout, stderr)
	case "changes":
		return runChanges(ctx, cfg, args[1:], stdout, stderr)
	case "novedades":
		return runNovedades(ctx, cfg, args[1:], stdout, stderr)
	case "next":
		return runNext(ctx, cfg, args[1:], stdout, stderr)
	case "advance":
		return runAdvance(ctx, cfg, args[1:], stdout, stderr)
	case "inbox":
		return runInbox(ctx, cfg, args[1:], stdout, stderr)
	case "resolve":
		return runResolve(ctx, cfg, args[1:], stdout, stderr)
	case "comment":
		return runComment(ctx, cfg, args[1:], stdout, stderr)
	case "project":
		return runProject(ctx, cfg, args[1:], stdout, stderr)
	case "phase":
		return runPhase(ctx, cfg, args[1:], stdout, stderr)
	case "epic":
		return runEpic(ctx, cfg, args[1:], stdout, stderr)
	case "doc":
		return runDoc(ctx, cfg, args[1:], stdout, stderr)
	case "import":
		return runImport(ctx, cfg, args[1:], stdout, stderr)
	case "audit":
		return runAudit(ctx, cfg, args[1:], stdout, stderr)
	case "admin":
		return runAdmin(ctx, cfg, args[1:], stdout, stderr)
	default:
		return fmt.Errorf("comando desconocido %q; usá agentbank help", args[0])
	}
}

func shouldEnsureAgent(command string) bool {
	switch command {
	case "login", "init", "agent", "worker", "project", "phase", "epic", "audit", "admin":
		return false
	default:
		return true
	}
}

func printUsage(w io.Writer) {
	fmt.Fprintln(w, `AgentBank Go CLI

Uso:
  agentbank serve                 arranca el servidor
  agentbank mcp                   servidor MCP por stdio
  agentbank login [flags]         guarda el token humano (una vez)
  agentbank init [flags]          escribe .agentbank.json y configuración
  agentbank agent list|ensure|rotate|revoke|disable
  agentbank api [flags]           llamada HTTP genérica (escape hatch)
  agentbank me                    identidad + briefing
  agentbank changes [flags]       feed incremental
  agentbank novedades [flags]     long-poll
  agentbank next [flags]          siguiente ticket (claim)
  agentbank advance [flags]       avanzar/cerrar ticket
  agentbank inbox [flags]         bandeja
  agentbank resolve [flags]       resolver mensaje
  agentbank comment [flags]       comentar ticket
  agentbank project list|create|get|edit|pause|resume|members|roles
  agentbank phase list|create|get|edit
  agentbank epic list|create|get|edit
  agentbank doc list|create|get|version|versions|diff|sync
  agentbank import --project ID archivo.json [--dry-run] [--update]
  agentbank audit chains|list|verify
  agentbank admin sweeps|metrics|verify-all|outbox|dispatch
  agentbank worker [--agent A] [--cmd CMD] [--role R] [--max N]

Configuración:
  AGENTBANK_URL       default http://127.0.0.1:8090
  AGENTBANK_TOKEN     token bearer
  AGENTBANK_PROJECT   proyecto por defecto
  ~/.config/agentbank/config.json  {server, project}
  ~/.config/agentbank/token        token (0600)
  .agentbank.json                  {server, project} sin secretos`)
}

// LoadConfig expone la configuración para el servidor MCP.
func LoadConfig() (Config, error) { return loadConfig() }

func loadConfig() (Config, error) {
	cfg := Config{URL: "http://127.0.0.1:8090"}
	homeConfig := configDir()
	type storedConfig struct {
		Server    string `json:"server"`
		Project   string `json:"project"`
		Agent     string `json:"agent"`
		AgentRole string `json:"agent_role"`
	}
	apply := func(stored storedConfig) {
		if stored.Server != "" {
			cfg.URL = stored.Server
		}
		if stored.Project != "" {
			cfg.Project = stored.Project
		}
		if stored.Agent != "" {
			cfg.Agent = stored.Agent
		}
		if stored.AgentRole != "" {
			cfg.AgentRole = stored.AgentRole
		}
	}
	if raw, err := os.ReadFile(filepath.Join(homeConfig, "config.json")); err == nil {
		var stored storedConfig
		if json.Unmarshal(raw, &stored) == nil {
			apply(stored)
		}
	}
	if raw, err := os.ReadFile(".agentbank.json"); err == nil {
		var stored storedConfig
		if json.Unmarshal(raw, &stored) == nil {
			apply(stored)
		}
	}
	if value := strings.TrimSpace(os.Getenv("AGENTBANK_URL")); value != "" {
		cfg.URL = value
	}
	if value := strings.TrimSpace(os.Getenv("AGENTBANK_PROJECT")); value != "" {
		cfg.Project = value
	}
	if value := strings.TrimSpace(os.Getenv("AGENTBANK_AGENT")); value != "" {
		cfg.Agent = value
	}
	if value := strings.TrimSpace(os.Getenv("AGENTBANK_AGENT_ROLE")); value != "" {
		cfg.AgentRole = value
	}
	if value := strings.TrimSpace(os.Getenv("AGENTBANK_HUMAN_TOKEN")); value != "" {
		cfg.HumanToken = value
	} else if value := strings.TrimSpace(os.Getenv("AGENTBANK_TOKEN")); value != "" {
		cfg.HumanToken = value
	} else {
		for _, name := range []string{"human.token", "token", "humano.token"} {
			if raw, err := os.ReadFile(filepath.Join(homeConfig, name)); err == nil {
				cfg.HumanToken = strings.TrimSpace(string(raw))
				break
			}
		}
	}
	cfg.Token = cfg.HumanToken
	if cfg.Agent != "" {
		if token := credentials.ReadToken(credentials.TokenPath(cfg.Agent)); token != "" {
			cfg.Token = token
		}
	}
	return cfg, nil
}

func configDir() string {
	return credentials.ConfigDir()
}

func newClient(cfg Config) *client.Client {
	return client.New(cfg.URL, cfg.Token, cfg.Project)
}

func writeJSON(w io.Writer, value any) error {
	encoded, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(w, string(encoded))
	return err
}

func runInit(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("init", flag.ContinueOnError)
	urlFlag := fs.String("url", "http://127.0.0.1:8090", "URL del server")
	project := fs.String("project", "", "slug o id del proyecto")
	token := fs.String("token", "", "token humano a guardar")
	tokenFile := fs.String("token-file", "", "archivo con el token humano")
	agent := fs.String("agent", "", "agente por defecto")
	role := fs.String("role", "", "rol funcional del agente")
	if err := fs.Parse(args); err != nil {
		return err
	}
	document := map[string]string{
		"server": *urlFlag, "project": *project,
		"agent": *agent, "agent_role": *role,
	}
	encoded, _ := json.MarshalIndent(document, "", "  ")
	if err := os.WriteFile(".agentbank.json", append(encoded, '\n'), 0o644); err != nil {
		return fmt.Errorf("escribir .agentbank.json: %w", err)
	}
	home := configDir()
	if err := os.MkdirAll(home, 0o700); err != nil {
		return fmt.Errorf("crear config dir: %w", err)
	}
	if err := os.WriteFile(filepath.Join(home, "config.json"), append(encoded, '\n'), 0o600); err != nil {
		return fmt.Errorf("escribir config global: %w", err)
	}
	tokenValue := strings.TrimSpace(*token)
	if *tokenFile != "" {
		raw, err := os.ReadFile(*tokenFile)
		if err != nil {
			return fmt.Errorf("leer token-file: %w", err)
		}
		tokenValue = strings.TrimSpace(string(raw))
	}
	if tokenValue != "" {
		if err := credentials.WriteToken(credentials.HumanTokenPath(), tokenValue); err != nil {
			return fmt.Errorf("guardar token humano: %w", err)
		}
	}
	fmt.Fprintln(stdout, "OK: .agentbank.json y configuración local escritos.")
	fmt.Fprintln(stdout, "Siguiente: agentbank me")
	return nil
}

func runAPI(ctx context.Context, cfg Config, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("api", flag.ContinueOnError)
	fs.SetOutput(stderr)
	method := fs.String("method", "GET", "método HTTP")
	path := fs.String("path", "", "path, por ejemplo /v1/projects")
	body := fs.String("body", "", "JSON inline o @archivo")
	idempotency := fs.String("idempotency-key", "", "clave de idempotencia")
	var queries, headers stringList
	fs.Var(&queries, "query", "k=v (repetible)")
	fs.Var(&headers, "header", "k=v (repetible)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *path == "" {
		return errors.New("falta --path")
	}
	var payload any
	if *body != "" {
		raw := *body
		if strings.HasPrefix(raw, "@") {
			content, err := os.ReadFile(strings.TrimPrefix(raw, "@"))
			if err != nil {
				return err
			}
			raw = string(content)
		}
		if err := json.Unmarshal([]byte(raw), &payload); err != nil {
			return fmt.Errorf("body JSON inválido: %w", err)
		}
	}
	headerMap := make(map[string]string)
	for _, header := range headers {
		parts := strings.SplitN(header, "=", 2)
		if len(parts) == 2 {
			headerMap[parts[0]] = parts[1]
		}
	}
	if *idempotency != "" {
		headerMap["Idempotency-Key"] = *idempotency
	}
	query := url.Values{}
	for _, item := range queries {
		parts := strings.SplitN(item, "=", 2)
		if len(parts) == 2 {
			query.Add(parts[0], parts[1])
		}
	}
	result, err := newClient(cfg).Do(ctx, strings.ToUpper(*method), *path, query, payload, headerMap)
	if err != nil {
		return err
	}
	if result == nil {
		fmt.Fprintln(stdout, "OK")
		return nil
	}
	return writeJSON(stdout, result)
}

func runMe(ctx context.Context, cfg Config, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("me", flag.ContinueOnError)
	fs.SetOutput(stderr)
	project := fs.String("project", cfg.Project, "proyecto para briefing")
	if err := fs.Parse(args); err != nil {
		return err
	}
	query := url.Values{}
	if *project != "" {
		query.Set("proyecto", *project)
	}
	result, err := newClient(cfg).Do(ctx, "GET", "/v1/me", query, nil, nil)
	if err != nil {
		return err
	}
	return writeJSON(stdout, result)
}

func runChanges(ctx context.Context, cfg Config, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("changes", flag.ContinueOnError)
	fs.SetOutput(stderr)
	since := fs.String("since", "", "cursor")
	project := fs.String("project", cfg.Project, "proyecto")
	limit := fs.Int("limit", 100, "máximo")
	if err := fs.Parse(args); err != nil {
		return err
	}
	query := url.Values{}
	if *since != "" {
		query.Set("since", *since)
	}
	if *project != "" {
		query.Set("project_id", *project)
	}
	query.Set("limit", fmt.Sprint(*limit))
	result, err := newClient(cfg).Do(ctx, "GET", "/v1/changes", query, nil, nil)
	if err != nil {
		return err
	}
	return writeJSON(stdout, result)
}

func runNovedades(ctx context.Context, cfg Config, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("novedades", flag.ContinueOnError)
	fs.SetOutput(stderr)
	since := fs.String("since", "", "cursor")
	wait := fs.Int("wait", 20, "segundos de long-poll")
	if err := fs.Parse(args); err != nil {
		return err
	}
	query := url.Values{}
	if *since != "" {
		query.Set("since", *since)
	}
	query.Set("wait", fmt.Sprint(*wait))
	result, err := newClient(cfg).Do(ctx, "GET", "/v1/novedades", query, nil, nil)
	if err != nil {
		return err
	}
	return writeJSON(stdout, result)
}

func runNext(ctx context.Context, cfg Config, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("next", flag.ContinueOnError)
	fs.SetOutput(stderr)
	project := fs.String("project", cfg.Project, "proyecto")
	role := fs.String("role", "", "rol")
	claim := fs.Bool("claim", true, "reclamar")
	if err := fs.Parse(args); err != nil {
		return err
	}
	body := map[string]any{"claim": *claim}
	if *project != "" {
		body["project_id"] = *project
	}
	if *role != "" {
		body["role"] = *role
	}
	result, err := newClient(cfg).Do(ctx, "POST", "/v1/tickets/siguiente", nil, body, nil)
	if err != nil {
		return err
	}
	return writeJSON(stdout, result)
}

func runAdvance(ctx context.Context, cfg Config, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("advance", flag.ContinueOnError)
	fs.SetOutput(stderr)
	id := fs.String("id", "", "ticket UUID, PREFIX-N o key")
	status := fs.String("status", "", "nuevo estado")
	version := fs.Int64("version", 0, "versión")
	note := fs.String("nota-cierre", "", "nota de cierre")
	comment := fs.String("comment", "", "comentario")
	question := fs.String("pregunta", "", "JSON de pregunta")
	next := fs.Bool("y-siguiente", false, "reclamar siguiente")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *id == "" {
		return errors.New("falta --id")
	}
	body := map[string]any{"y_siguiente": *next}
	if *status != "" {
		body["status"] = *status
	}
	if *version > 0 {
		body["version"] = *version
	}
	if *note != "" {
		body["nota_cierre"] = *note
	}
	if *comment != "" {
		body["comment"] = *comment
	}
	if *question != "" {
		var parsed any
		if err := json.Unmarshal([]byte(*question), &parsed); err != nil {
			return fmt.Errorf("pregunta JSON inválida: %w", err)
		}
		body["pregunta"] = parsed
	}
	result, err := newClient(cfg).Do(ctx, "POST", "/v1/tickets/"+url.PathEscape(*id)+"/avanzar", nil, body, nil)
	if err != nil {
		return err
	}
	return writeJSON(stdout, result)
}

func runInbox(ctx context.Context, cfg Config, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("inbox", flag.ContinueOnError)
	fs.SetOutput(stderr)
	project := fs.String("project", cfg.Project, "proyecto")
	pending := fs.Bool("pending", true, "solo pendientes")
	if err := fs.Parse(args); err != nil {
		return err
	}
	query := url.Values{}
	if *project != "" {
		query.Set("project_id", *project)
	}
	if *pending {
		query.Set("pendientes", "1")
	}
	result, err := newClient(cfg).Do(ctx, "GET", "/v1/me/bandeja", query, nil, nil)
	if err != nil {
		return err
	}
	return writeJSON(stdout, result)
}

func runResolve(ctx context.Context, cfg Config, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("resolve", flag.ContinueOnError)
	fs.SetOutput(stderr)
	id := fs.String("id", "", "message id")
	option := fs.Int("option", -1, "índice de opción")
	text := fs.String("text", "", "texto")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *id == "" {
		return errors.New("falta --id")
	}
	body := map[string]any{}
	if *option >= 0 {
		body["option"] = *option
	}
	if *text != "" {
		body["text"] = *text
	}
	result, err := newClient(cfg).Do(ctx, "POST", "/v1/messages/"+url.PathEscape(*id)+"/resolver", nil, body, nil)
	if err != nil {
		return err
	}
	return writeJSON(stdout, result)
}

func runComment(ctx context.Context, cfg Config, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("comment", flag.ContinueOnError)
	fs.SetOutput(stderr)
	id := fs.String("id", "", "ticket")
	bodyText := fs.String("body", "", "texto")
	ctype := fs.String("type", "comentario", "comentario|nota|cierre")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *id == "" || *bodyText == "" {
		return errors.New("faltan --id y --body")
	}
	result, err := newClient(cfg).Do(ctx, "POST", "/v1/tickets/"+url.PathEscape(*id)+"/comments", nil,
		map[string]any{"body": *bodyText, "type": *ctype}, nil)
	if err != nil {
		return err
	}
	return writeJSON(stdout, result)
}

func runProject(ctx context.Context, cfg Config, args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		return errors.New("uso: agentbank project list|create|get|edit|pause|resume|members|roles")
	}
	action := args[0]
	switch action {
	case "list":
		result, err := newClient(cfg).Do(ctx, "GET", "/v1/projects", nil, nil, nil)
		if err != nil {
			return err
		}
		return writeJSON(stdout, result)
	case "create":
		fs := flag.NewFlagSet("project create", flag.ContinueOnError)
		fs.SetOutput(stderr)
		slug := fs.String("slug", "", "slug")
		name := fs.String("name", "", "nombre")
		prefix := fs.String("prefix", "", "prefijo")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		body := map[string]any{"slug": *slug, "name": *name}
		if *prefix != "" {
			body["prefix"] = *prefix
		}
		result, err := newClient(cfg).Do(ctx, "POST", "/v1/projects", nil, body, nil)
		if err != nil {
			return err
		}
		return writeJSON(stdout, result)
	case "get":
		fs := flag.NewFlagSet("project get", flag.ContinueOnError)
		fs.SetOutput(stderr)
		id := fs.String("id", "", "id o slug")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		result, err := newClient(cfg).Do(ctx, "GET", "/v1/projects/"+url.PathEscape(*id), nil, nil, nil)
		if err != nil {
			return err
		}
		return writeJSON(stdout, result)
	case "pause", "resume":
		fs := flag.NewFlagSet("project "+action, flag.ContinueOnError)
		fs.SetOutput(stderr)
		id := fs.String("id", cfg.Project, "id o slug")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		path := "/v1/projects/" + url.PathEscape(*id) + "/seguir"
		if action == "pause" {
			path = "/v1/projects/" + url.PathEscape(*id) + "/pausa"
		}
		result, err := newClient(cfg).Do(ctx, "POST", path, nil, nil, nil)
		if err != nil {
			return err
		}
		return writeJSON(stdout, result)
	case "members":
		fs := flag.NewFlagSet("project members", flag.ContinueOnError)
		fs.SetOutput(stderr)
		id := fs.String("id", cfg.Project, "id o slug")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		result, err := newClient(cfg).Do(ctx, "GET", "/v1/projects/"+url.PathEscape(*id)+"/members", nil, nil, nil)
		if err != nil {
			return err
		}
		return writeJSON(stdout, result)
	case "roles":
		fs := flag.NewFlagSet("project roles", flag.ContinueOnError)
		fs.SetOutput(stderr)
		id := fs.String("id", cfg.Project, "id o slug")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		result, err := newClient(cfg).Do(ctx, "GET", "/v1/projects/"+url.PathEscape(*id)+"/roles", nil, nil, nil)
		if err != nil {
			return err
		}
		return writeJSON(stdout, result)
	default:
		return fmt.Errorf("acción desconocida project %q", action)
	}
}

func runPhase(ctx context.Context, cfg Config, args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		return errors.New("uso: agentbank phase list|create|get|edit")
	}
	action := args[0]
	switch action {
	case "list":
		fs := flag.NewFlagSet("phase list", flag.ContinueOnError)
		fs.SetOutput(stderr)
		project := fs.String("project", cfg.Project, "proyecto")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		result, err := newClient(cfg).Do(ctx, "GET", "/v1/projects/"+url.PathEscape(*project)+"/phases", nil, nil, nil)
		if err != nil {
			return err
		}
		return writeJSON(stdout, result)
	case "create":
		fs := flag.NewFlagSet("phase create", flag.ContinueOnError)
		fs.SetOutput(stderr)
		project := fs.String("project", cfg.Project, "proyecto")
		key := fs.String("key", "", "key")
		name := fs.String("name", "", "nombre")
		order := fs.Int64("order", 0, "orden")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		body := map[string]any{"name": *name}
		if *key != "" {
			body["key"] = *key
		}
		if *order != 0 {
			body["order_index"] = *order
		}
		result, err := newClient(cfg).Do(ctx, "POST", "/v1/projects/"+url.PathEscape(*project)+"/phases", nil, body, nil)
		if err != nil {
			return err
		}
		return writeJSON(stdout, result)
	case "get", "edit":
		fs := flag.NewFlagSet("phase "+action, flag.ContinueOnError)
		fs.SetOutput(stderr)
		id := fs.String("id", "", "fase")
		status := fs.String("status", "", "abierta|cerrada")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if action == "get" {
			result, err := newClient(cfg).Do(ctx, "GET", "/v1/phases/"+url.PathEscape(*id), nil, nil, nil)
			if err != nil {
				return err
			}
			return writeJSON(stdout, result)
		}
		body := map[string]any{}
		if *status != "" {
			body["status"] = *status
		}
		result, err := newClient(cfg).Do(ctx, "PATCH", "/v1/phases/"+url.PathEscape(*id), nil, body, nil)
		if err != nil {
			return err
		}
		return writeJSON(stdout, result)
	default:
		return fmt.Errorf("acción desconocida phase %q", action)
	}
}

func runEpic(ctx context.Context, cfg Config, args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		return errors.New("uso: agentbank epic list|create|get|edit")
	}
	action := args[0]
	switch action {
	case "list":
		fs := flag.NewFlagSet("epic list", flag.ContinueOnError)
		fs.SetOutput(stderr)
		project := fs.String("project", cfg.Project, "proyecto")
		phase := fs.String("phase", "", "fase")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		query := url.Values{}
		if *phase != "" {
			query.Set("phase_id", *phase)
		}
		result, err := newClient(cfg).Do(ctx, "GET", "/v1/projects/"+url.PathEscape(*project)+"/epics", query, nil, nil)
		if err != nil {
			return err
		}
		return writeJSON(stdout, result)
	case "create":
		fs := flag.NewFlagSet("epic create", flag.ContinueOnError)
		fs.SetOutput(stderr)
		project := fs.String("project", cfg.Project, "proyecto")
		key := fs.String("key", "", "key")
		title := fs.String("title", "", "título")
		phase := fs.String("phase", "", "fase")
		order := fs.Int64("order", 0, "orden")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		body := map[string]any{"title": *title}
		if *key != "" {
			body["key"] = *key
		}
		if *phase != "" {
			body["phase_id"] = *phase
		}
		if *order != 0 {
			body["order_index"] = *order
		}
		result, err := newClient(cfg).Do(ctx, "POST", "/v1/projects/"+url.PathEscape(*project)+"/epics", nil, body, nil)
		if err != nil {
			return err
		}
		return writeJSON(stdout, result)
	case "get", "edit":
		fs := flag.NewFlagSet("epic "+action, flag.ContinueOnError)
		fs.SetOutput(stderr)
		id := fs.String("id", "", "épica")
		status := fs.String("status", "", "abierta|cerrada")
		phase := fs.String("phase", "", "fase")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if action == "get" {
			result, err := newClient(cfg).Do(ctx, "GET", "/v1/epics/"+url.PathEscape(*id), nil, nil, nil)
			if err != nil {
				return err
			}
			return writeJSON(stdout, result)
		}
		body := map[string]any{}
		if *status != "" {
			body["status"] = *status
		}
		if *phase != "" {
			body["phase_id"] = *phase
		}
		result, err := newClient(cfg).Do(ctx, "PATCH", "/v1/epics/"+url.PathEscape(*id), nil, body, nil)
		if err != nil {
			return err
		}
		return writeJSON(stdout, result)
	default:
		return fmt.Errorf("acción desconocida epic %q", action)
	}
}

func runDoc(ctx context.Context, cfg Config, args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		return errors.New("uso: agentbank doc list|create|get|version|versions|diff|sync")
	}
	action := args[0]
	switch action {
	case "list":
		fs := flag.NewFlagSet("doc list", flag.ContinueOnError)
		fs.SetOutput(stderr)
		project := fs.String("project", cfg.Project, "proyecto")
		updated := fs.String("updated-since", "", "solo cambios")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		query := url.Values{}
		if *updated != "" {
			query.Set("updated_since", *updated)
		}
		result, err := newClient(cfg).Do(ctx, "GET", "/v1/projects/"+url.PathEscape(*project)+"/documents", query, nil, nil)
		if err != nil {
			return err
		}
		return writeJSON(stdout, result)
	case "create":
		fs := flag.NewFlagSet("doc create", flag.ContinueOnError)
		fs.SetOutput(stderr)
		project := fs.String("project", cfg.Project, "proyecto")
		title := fs.String("title", "", "título")
		route := fs.String("route", "", "ruta")
		file := fs.String("file", "", "archivo markdown")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		content, err := os.ReadFile(*file)
		if err != nil {
			return err
		}
		body := map[string]any{"title": *title, "content": string(content)}
		if *route != "" {
			body["route"] = *route
		}
		result, err := newClient(cfg).Do(ctx, "POST", "/v1/projects/"+url.PathEscape(*project)+"/documents", nil, body, nil)
		if err != nil {
			return err
		}
		return writeJSON(stdout, result)
	case "get", "versions":
		fs := flag.NewFlagSet("doc "+action, flag.ContinueOnError)
		fs.SetOutput(stderr)
		id := fs.String("id", "", "documento")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		path := "/v1/documents/" + url.PathEscape(*id)
		if action == "versions" {
			path += "/versions"
		}
		result, err := newClient(cfg).Do(ctx, "GET", path, nil, nil, nil)
		if err != nil {
			return err
		}
		return writeJSON(stdout, result)
	case "version":
		fs := flag.NewFlagSet("doc version", flag.ContinueOnError)
		fs.SetOutput(stderr)
		id := fs.String("id", "", "documento")
		file := fs.String("file", "", "archivo markdown")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		content, err := os.ReadFile(*file)
		if err != nil {
			return err
		}
		result, err := newClient(cfg).Do(ctx, "POST", "/v1/documents/"+url.PathEscape(*id)+"/versions", nil,
			map[string]any{"content": string(content)}, nil)
		if err != nil {
			return err
		}
		return writeJSON(stdout, result)
	case "diff":
		fs := flag.NewFlagSet("doc diff", flag.ContinueOnError)
		fs.SetOutput(stderr)
		id := fs.String("id", "", "documento")
		from := fs.String("from", "", "versión desde")
		to := fs.String("to", "", "versión hasta")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		query := url.Values{}
		if *from != "" {
			query.Set("from", *from)
		}
		if *to != "" {
			query.Set("to", *to)
		}
		result, err := newClient(cfg).Do(ctx, "GET", "/v1/documents/"+url.PathEscape(*id)+"/diff", query, nil, nil)
		if err != nil {
			return err
		}
		return writeJSON(stdout, result)
	case "sync":
		fs := flag.NewFlagSet("doc sync", flag.ContinueOnError)
		fs.SetOutput(stderr)
		project := fs.String("project", cfg.Project, "proyecto")
		dir := fs.String("dir", ".", "carpeta")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		documents := make([]map[string]any, 0, 16)
		err := filepath.WalkDir(*dir, func(path string, entry os.DirEntry, err error) error {
			if err != nil || entry.IsDir() || !strings.HasSuffix(strings.ToLower(path), ".md") {
				return err
			}
			content, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			route, _ := filepath.Rel(*dir, path)
			documents = append(documents, map[string]any{
				"route": filepath.ToSlash(route), "title": filepath.Base(path), "content": string(content),
			})
			return nil
		})
		if err != nil {
			return err
		}
		result, err := newClient(cfg).Do(ctx, "POST", "/v1/projects/"+url.PathEscape(*project)+"/documents/sync", nil,
			map[string]any{"documents": documents}, nil)
		if err != nil {
			return err
		}
		return writeJSON(stdout, result)
	default:
		return fmt.Errorf("acción desconocida doc %q", action)
	}
}

func runImport(ctx context.Context, cfg Config, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("import", flag.ContinueOnError)
	fs.SetOutput(stderr)
	project := fs.String("project", cfg.Project, "proyecto")
	dryRun := fs.Bool("dry-run", false, "no persistir")
	update := fs.Bool("update", false, "actualizar existentes")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return errors.New("uso: agentbank import --project ID archivo.json")
	}
	raw, err := os.ReadFile(fs.Arg(0))
	if err != nil {
		return err
	}
	var body any
	if err := json.Unmarshal(raw, &body); err != nil {
		return fmt.Errorf("JSON inválido: %w", err)
	}
	query := url.Values{}
	if *dryRun {
		query.Set("dry_run", "1")
	}
	if *update {
		query.Set("modo", "actualizar")
	}
	result, err := newClient(cfg).Do(ctx, "POST", "/v1/projects/"+url.PathEscape(*project)+"/import", query, body, nil)
	if err != nil {
		return err
	}
	return writeJSON(stdout, result)
}

func runAudit(ctx context.Context, cfg Config, args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		return errors.New("uso: agentbank audit chains|list|verify")
	}
	switch args[0] {
	case "chains":
		result, err := newClient(cfg).Do(ctx, "GET", "/v1/audit/chains", nil, nil, nil)
		if err != nil {
			return err
		}
		return writeJSON(stdout, result)
	case "list":
		fs := flag.NewFlagSet("audit list", flag.ContinueOnError)
		fs.SetOutput(stderr)
		chain := fs.String("chain", "", "chain_id")
		limit := fs.Int("limit", 50, "máximo")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		query := url.Values{}
		if *chain != "" {
			query.Set("chain_id", *chain)
		}
		query.Set("limit", fmt.Sprint(*limit))
		result, err := newClient(cfg).Do(ctx, "GET", "/v1/audit", query, nil, nil)
		if err != nil {
			return err
		}
		return writeJSON(stdout, result)
	case "verify":
		fs := flag.NewFlagSet("audit verify", flag.ContinueOnError)
		fs.SetOutput(stderr)
		chain := fs.String("chain", "", "chain_id")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		query := url.Values{}
		query.Set("chain_id", *chain)
		result, err := newClient(cfg).Do(ctx, "GET", "/v1/audit/verify", query, nil, nil)
		if err != nil {
			return err
		}
		return writeJSON(stdout, result)
	default:
		return fmt.Errorf("acción desconocida audit %q", args[0])
	}
}

func runAdmin(ctx context.Context, cfg Config, args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		return errors.New("uso: agentbank admin sweeps|metrics|verify-all|outbox|dispatch")
	}
	switch args[0] {
	case "sweeps":
		result, err := newClient(cfg).Do(ctx, "POST", "/v1/admin/barridos", nil, nil, nil)
		if err != nil {
			return err
		}
		return writeJSON(stdout, result)
	case "metrics":
		result, err := newClient(cfg).Do(ctx, "GET", "/v1/admin/metrics", nil, nil, nil)
		if err != nil {
			return err
		}
		return writeJSON(stdout, result)
	case "verify-all":
		result, err := newClient(cfg).Do(ctx, "POST", "/v1/admin/verify-all", nil, nil, nil)
		if err != nil {
			return err
		}
		return writeJSON(stdout, result)
	case "outbox":
		result, err := newClient(cfg).Do(ctx, "GET", "/v1/admin/outbox", nil, nil, nil)
		if err != nil {
			return err
		}
		return writeJSON(stdout, result)
	case "dispatch":
		result, err := newClient(cfg).Do(ctx, "POST", "/v1/admin/outbox/despachar", nil, nil, nil)
		if err != nil {
			return err
		}
		return writeJSON(stdout, result)
	default:
		return fmt.Errorf("acción desconocida admin %q", args[0])
	}
}

func runWorker(ctx context.Context, cfg Config, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("worker", flag.ContinueOnError)
	fs.SetOutput(stderr)
	project := fs.String("project", cfg.Project, "proyecto")
	role := fs.String("role", "", "rol de trabajo")
	agentName := fs.String("agent", cfg.Agent, "nombre del agente")
	agentRole := fs.String("agent-role", cfg.AgentRole, "rol funcional del agente")
	command := fs.String("cmd", "", "comando por ticket")
	max := fs.Int("max", 0, "máximo de tickets (0 = infinito)")
	sleep := fs.Duration("sleep", 5*time.Second, "espera si no hay tickets")
	simulated := fs.Bool("simulado", false, "no ejecuta comando")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *command == "" && !*simulated {
		return errors.New("falta --cmd o --simulado")
	}
	if *agentName != "" {
		effectiveRole := *agentRole
		if effectiveRole == "" {
			effectiveRole = *role
		}
		result, err := agentruntime.Ensure(ctx, agentruntime.Options{
			BaseURL: cfg.URL, HumanToken: cfg.HumanToken, Project: *project,
			Agent: *agentName, Role: effectiveRole,
		})
		if err != nil {
			return err
		}
		cfg.Token = result.Token
	} else if cfg.Token == "" {
		cfg.Token = cfg.HumanToken
	}
	processed := 0
	for *max == 0 || processed < *max {
		select {
		case <-ctx.Done():
			return nil
		default:
		}
		body := map[string]any{"claim": true}
		if *project != "" {
			body["project_id"] = *project
		}
		if *role != "" {
			body["role"] = *role
		}
		result, err := newClient(cfg).Do(ctx, "POST", "/v1/tickets/siguiente", nil, body, nil)
		if err != nil {
			return err
		}
		payload, _ := result.(map[string]any)
		ticket, _ := payload["ticket"].(map[string]any)
		if ticket == nil {
			fmt.Fprintf(stderr, "sin tickets (%v); esperando %s\n", payload["motivo"], *sleep)
			select {
			case <-ctx.Done():
				return nil
			case <-time.After(*sleep):
				continue
			}
		}
		ticketID, _ := ticket["id"].(string)
		ticketRef, _ := ticket["ref"].(string)
		fmt.Fprintf(stderr, "ticket %s (%s)\n", ticketRef, ticketID)
		if *simulated {
			select {
			case <-ctx.Done():
			case <-time.After(time.Second):
			}
			_, _ = newClient(cfg).Do(ctx, "POST", "/v1/tickets/"+url.PathEscape(ticketID)+"/claims/release", nil, nil, nil)
		} else {
			runCtx, cancel := context.WithCancel(ctx)
			heartbeatDone := make(chan struct{})
			go func() {
				ticker := time.NewTicker(time.Minute)
				defer ticker.Stop()
				defer close(heartbeatDone)
				for {
					select {
					case <-runCtx.Done():
						return
					case <-ticker.C:
						_, _ = newClient(cfg).Do(runCtx, "POST", "/v1/me/heartbeat", nil, nil, nil)
					}
				}
			}()
			cmd := exec.CommandContext(runCtx, "/bin/sh", "-lc", *command)
			cmd.Stdout = stdout
			cmd.Stderr = stderr
			cmd.Env = append(os.Environ(),
				"AGENTBANK_URL="+cfg.URL,
				"AGENTBANK_TOKEN="+cfg.Token,
				"AGENTBANK_PROJECT="+firstNonEmpty(*project, cfg.Project),
				"AGENTBANK_TICKET="+ticketRef,
				"AGENTBANK_TICKET_ID="+ticketID,
				"AGENTBANK_TICKET_REF="+ticketRef,
			)
			runErr := cmd.Run()
			cancel()
			<-heartbeatDone
			if runErr != nil {
				_, _ = newClient(cfg).Do(ctx, "POST", "/v1/tickets/"+url.PathEscape(ticketID)+"/comments", nil,
					map[string]any{"body": "worker: comando falló: " + runErr.Error(), "type": "nota"}, nil)
				_, _ = newClient(cfg).Do(ctx, "POST", "/v1/tickets/"+url.PathEscape(ticketID)+"/claims/release", nil, nil, nil)
			} else {
				current, _ := newClient(cfg).Do(ctx, "GET", "/v1/tickets/"+url.PathEscape(ticketID), nil, nil, nil)
				currentMap, _ := current.(map[string]any)
				if currentMap["status"] == "en curso" {
					_, _ = newClient(cfg).Do(ctx, "POST", "/v1/tickets/"+url.PathEscape(ticketID)+"/comments", nil,
						map[string]any{"body": "el agente terminó sin cerrar", "type": "nota"}, nil)
					_, _ = newClient(cfg).Do(ctx, "POST", "/v1/tickets/"+url.PathEscape(ticketID)+"/claims/release", nil, nil, nil)
				}
			}
		}
		processed++
	}
	return nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func runLogin(ctx context.Context, cfg Config, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("login", flag.ContinueOnError)
	fs.SetOutput(stderr)
	urlFlag := fs.String("url", cfg.URL, "URL del server")
	token := fs.String("token", "", "token humano")
	project := fs.String("project", cfg.Project, "proyecto por defecto")
	if err := fs.Parse(args); err != nil {
		return err
	}
	value := strings.TrimSpace(*token)
	if value == "" {
		value = cfg.HumanToken
	}
	if value == "" {
		return errors.New("falta --token o AGENTBANK_TOKEN")
	}
	api := client.New(*urlFlag, value, *project)
	if _, err := api.Do(ctx, "GET", "/v1/me", nil, nil, nil); err != nil {
		return fmt.Errorf("el token humano no es válido: %w", err)
	}
	if err := credentials.WriteToken(credentials.HumanTokenPath(), value); err != nil {
		return err
	}
	home := credentials.ConfigDir()
	if err := os.MkdirAll(home, 0o700); err != nil {
		return err
	}
	document, _ := json.MarshalIndent(map[string]string{"server": *urlFlag, "project": *project}, "", "  ")
	if err := os.WriteFile(filepath.Join(home, "config.json"), append(document, '\n'), 0o600); err != nil {
		return err
	}
	fmt.Fprintln(stdout, "OK: token humano guardado en ~/.config/agentbank/human.token")
	return nil
}

func runAgent(ctx context.Context, cfg Config, args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		return errors.New("uso: agentbank agent list|ensure|rotate|revoke|disable")
	}
	if cfg.HumanToken == "" {
		return errors.New("falta token humano; corré agentbank login")
	}
	human := client.New(cfg.URL, cfg.HumanToken, cfg.Project)
	action := args[0]
	switch action {
	case "list":
		fs := flag.NewFlagSet("agent list", flag.ContinueOnError)
		fs.SetOutput(stderr)
		role := fs.String("role", "", "filtrar por rol")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		query := url.Values{}
		query.Set("type", "agente")
		query.Set("limit", "200")
		if *role != "" {
			query.Set("role", *role)
		}
		result, err := human.Do(ctx, "GET", "/v1/participants", query, nil, nil)
		if err != nil {
			return err
		}
		return writeJSON(stdout, result)
	case "ensure":
		fs := flag.NewFlagSet("agent ensure", flag.ContinueOnError)
		fs.SetOutput(stderr)
		agent := fs.String("agent", cfg.Agent, "nombre del agente")
		role := fs.String("role", cfg.AgentRole, "rol funcional")
		project := fs.String("project", cfg.Project, "proyecto")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		result, err := agentruntime.Ensure(ctx, agentruntime.Options{
			BaseURL: cfg.URL, HumanToken: cfg.HumanToken, Project: *project,
			Agent: *agent, Role: *role,
		})
		if err != nil {
			return err
		}
		return writeJSON(stdout, result)
	case "rotate", "revoke", "disable":
		fs := flag.NewFlagSet("agent "+action, flag.ContinueOnError)
		fs.SetOutput(stderr)
		name := fs.String("agent", cfg.Agent, "nombre del agente")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if *name == "" {
			return errors.New("falta --agent")
		}
		id, err := agentIDByName(ctx, human, *name)
		if err != nil {
			return err
		}
		switch action {
		case "rotate":
			response, err := human.Do(ctx, "POST", "/v1/participants/"+url.PathEscape(id)+"/tokens", nil, nil, nil)
			if err != nil {
				return err
			}
			root, _ := response.(map[string]any)
			token, _ := root["token"].(string)
			if token == "" {
				return errors.New("la rotación no devolvió token")
			}
			if err := credentials.WriteToken(credentials.TokenPath(*name), token); err != nil {
				return err
			}
			return writeJSON(stdout, map[string]any{"agent": *name, "rotated": true})
		case "revoke":
			if _, err := human.Do(ctx, "POST", "/v1/participants/"+url.PathEscape(id)+"/tokens/revoke", nil, nil, nil); err != nil {
				return err
			}
			_ = credentials.RemoveToken(*name)
			return writeJSON(stdout, map[string]any{"agent": *name, "revoked": true})
		default:
			if _, err := human.Do(ctx, "POST", "/v1/participants/"+url.PathEscape(id)+"/baja", nil, nil, nil); err != nil {
				return err
			}
			_ = credentials.RemoveToken(*name)
			return writeJSON(stdout, map[string]any{"agent": *name, "status": "baja"})
		}
	default:
		return fmt.Errorf("acción desconocida agent %q", action)
	}
}

func agentIDByName(ctx context.Context, human *client.Client, name string) (string, error) {
	query := url.Values{}
	query.Set("type", "agente")
	query.Set("name", name)
	query.Set("limit", "20")
	response, err := human.Do(ctx, "GET", "/v1/participants", query, nil, nil)
	if err != nil {
		return "", err
	}
	root, _ := response.(map[string]any)
	items, _ := root["items"].([]any)
	for _, raw := range items {
		item, _ := raw.(map[string]any)
		if item["name"] == name {
			id, _ := item["id"].(string)
			if id != "" {
				return id, nil
			}
		}
	}
	return "", fmt.Errorf("no existe el agente %q", name)
}
