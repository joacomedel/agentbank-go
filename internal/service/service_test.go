package service_test

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/joacomedel/agentbank-go/internal/audit"
	"github.com/joacomedel/agentbank-go/internal/auth"
	"github.com/joacomedel/agentbank-go/internal/db"
	"github.com/joacomedel/agentbank-go/internal/domain"
	"github.com/joacomedel/agentbank-go/internal/service"
)

type fakeClock struct {
	value time.Time
}

func (c *fakeClock) Now() time.Time { return c.value }

func (c *fakeClock) Advance(step time.Duration) { c.value = c.value.Add(step) }

func newTestService(t *testing.T) (*service.Service, *fakeClock, *sql.DB) {
	t.Helper()
	handle, err := db.Open(context.Background(), filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { _ = handle.Close() })
	if err := db.Migrate(context.Background(), handle); err != nil {
		t.Fatalf("db.Migrate: %v", err)
	}
	clock := &fakeClock{value: time.Date(2026, 11, 7, 20, 0, 0, 0, time.UTC)}
	return service.New(handle, clock.Now), clock, handle
}

func bootstrap(t *testing.T, svc *service.Service) (service.Identity, string) {
	t.Helper()
	const token = "ab_bootstrap_token_de_prueba_0123456789abcdef"
	result, err := svc.Bootstrap(context.Background(), token)
	if err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	if !result.Created {
		t.Fatal("Bootstrap no creó al humano")
	}
	identity, err := svc.Authenticate(context.Background(), token)
	if err != nil {
		t.Fatalf("Authenticate bootstrap: %v", err)
	}
	return identity, token
}

func TestBootstrapStoresOnlyHashAndAudits(t *testing.T) {
	svc, _, database := newTestService(t)
	const token = "ab_bootstrap_token_de_prueba_0123456789abcdef"
	result, err := svc.Bootstrap(context.Background(), token)
	if err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	if !result.Created || result.ParticipantID == "" {
		t.Fatalf("resultado inesperado: %+v", result)
	}

	identity, err := svc.Authenticate(context.Background(), token)
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	if identity.Participant.Type != domain.ParticipantHuman || !identity.Participant.Admin {
		t.Fatalf("participante inesperado: %+v", identity.Participant)
	}
	if _, err := svc.Authenticate(context.Background(), "ab_otro_token_de_prueba_0123456789abcdefghij"); !errors.Is(err, service.ErrInvalidToken) {
		t.Fatalf("token desconocido: err=%v", err)
	}

	var hash string
	if err := database.QueryRow(`SELECT token_hash FROM tokens WHERE participant_id = ?`, identity.Participant.ID).Scan(&hash); err != nil {
		t.Fatalf("leer token: %v", err)
	}
	if hash == token || hash != auth.HashToken(token) {
		t.Fatalf("el token no está guardado solo como hash")
	}
	var plaintext int
	if err := database.QueryRow(`SELECT COUNT(1) FROM tokens WHERE token_hash = ?`, token).Scan(&plaintext); err != nil {
		t.Fatalf("contar texto plano: %v", err)
	}
	if plaintext != 0 {
		t.Fatal("se encontró el token en claro en la base")
	}

	resultVerify, err := audit.Verify(context.Background(), database, domain.SystemChain)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if !resultVerify.OK || resultVerify.Events != 2 {
		t.Fatalf("cadena system inesperada: %+v", resultVerify)
	}

	var actorID string
	if err := database.QueryRow(`
		SELECT actor_id FROM audit_events
		WHERE chain_id = 'system' AND action = 'bootstrap.humano'`,
	).Scan(&actorID); err != nil {
		t.Fatalf("leer auditoría: %v", err)
	}
	if actorID != identity.Participant.ID {
		t.Fatalf("actor de bootstrap=%q, want %q", actorID, identity.Participant.ID)
	}
	var emitted int
	if err := database.QueryRow(`
		SELECT COUNT(1) FROM audit_events
		WHERE chain_id = 'system' AND action = 'token.emitir'`,
	).Scan(&emitted); err != nil {
		t.Fatalf("contar token.emitir: %v", err)
	}
	if emitted != 1 {
		t.Fatalf("token.emitir=%d, want 1", emitted)
	}
}

func TestBootstrapRequiresTokenOnFirstStart(t *testing.T) {
	svc, _, _ := newTestService(t)
	if _, err := svc.Bootstrap(context.Background(), ""); err == nil {
		t.Fatal("Bootstrap aceptó token vacío en base vacía")
	}
	if _, err := svc.Bootstrap(context.Background(), "corto"); err == nil {
		t.Fatal("Bootstrap aceptó un token demasiado corto")
	}
}

func TestBootstrapIsIdempotent(t *testing.T) {
	svc, _, _ := newTestService(t)
	_, token := bootstrap(t, svc)
	result, err := svc.Bootstrap(context.Background(), "otro_token_de_bootstrap_0123456789abcdef")
	if err != nil {
		t.Fatalf("segundo Bootstrap: %v", err)
	}
	if result.Created {
		t.Fatal("el segundo Bootstrap creó otro humano")
	}
	if _, err := svc.Authenticate(context.Background(), token); err != nil {
		t.Fatalf("el token original debía seguir válido: %v", err)
	}
	if _, err := svc.Authenticate(context.Background(), "otro_token_de_bootstrap_0123456789abcdef"); !errors.Is(err, service.ErrInvalidToken) {
		t.Fatal("el token nuevo no debía existir")
	}
}

func TestTokenRotationAndRevocation(t *testing.T) {
	svc, fake, database := newTestService(t)
	human, _ := bootstrap(t, svc)
	created, err := svc.CreateParticipant(context.Background(), human, service.ParticipantInput{
		Name: "agente-1",
		Type: domain.ParticipantAgent,
	})
	if err != nil {
		t.Fatalf("CreateParticipant: %v", err)
	}
	if !auth.LooksLikeToken(created.Token) {
		t.Fatalf("token de agente con formato inválido: %q", created.Token)
	}
	agent, err := svc.Authenticate(context.Background(), created.Token)
	if err != nil {
		t.Fatalf("Authenticate agente: %v", err)
	}

	fake.Advance(time.Minute)
	newToken, newTokenID, err := svc.RotateToken(context.Background(), human, agent.Participant.ID)
	if err != nil {
		t.Fatalf("RotateToken: %v", err)
	}
	if newToken == created.Token || newTokenID == created.TokenID {
		t.Fatal("la rotación devolvió el mismo token")
	}
	if _, err := svc.Authenticate(context.Background(), created.Token); !errors.Is(err, service.ErrInvalidToken) {
		t.Fatalf("el token viejo debía estar revocado: %v", err)
	}
	if _, err := svc.Authenticate(context.Background(), newToken); err != nil {
		t.Fatalf("el token nuevo debía funcionar: %v", err)
	}
	var active int
	if err := database.QueryRow(`SELECT COUNT(1) FROM tokens WHERE participant_id = ? AND revoked_at IS NULL`, agent.Participant.ID).Scan(&active); err != nil {
		t.Fatalf("contar tokens activos: %v", err)
	}
	if active != 1 {
		t.Fatalf("tokens activos=%d, want 1", active)
	}

	fake.Advance(time.Minute)
	if _, err := svc.RevokeToken(context.Background(), human, newTokenID); err != nil {
		t.Fatalf("RevokeToken: %v", err)
	}
	if _, err := svc.Authenticate(context.Background(), newToken); !errors.Is(err, service.ErrInvalidToken) {
		t.Fatalf("el token revocado debía fallar: %v", err)
	}
	var conflict *service.ConflictError
	if _, err := svc.RevokeToken(context.Background(), human, newTokenID); !errors.As(err, &conflict) {
		t.Fatalf("revocar dos veces debía dar conflicto: %v", err)
	}
	verify, err := audit.Verify(context.Background(), database, domain.SystemChain)
	if err != nil || !verify.OK {
		t.Fatalf("cadena inválida: result=%+v err=%v", verify, err)
	}
}

func TestPermissions(t *testing.T) {
	svc, _, _ := newTestService(t)
	human, _ := bootstrap(t, svc)
	project, err := svc.CreateProject(context.Background(), human, service.ProjectInput{
		Slug: "proyecto-uno", Name: "Proyecto uno", Prefix: "PUNO",
	})
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	created, err := svc.CreateParticipant(context.Background(), human, service.ParticipantInput{
		Name: "agente-lector",
		Type: domain.ParticipantAgent,
		Projects: []service.ParticipantProjectInput{{
			ProjectID: project.Project.ID, Permission: domain.PermissionReader, Roles: []string{"backend"},
		}},
	})
	if err != nil {
		t.Fatalf("CreateParticipant: %v", err)
	}
	agent, err := svc.Authenticate(context.Background(), created.Token)
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	view, err := svc.GetProject(context.Background(), agent, project.Project.Slug)
	if err != nil {
		t.Fatalf("GetProject: %v", err)
	}
	if view.Permission != domain.PermissionReader {
		t.Fatalf("permission=%q", view.Permission)
	}
	if list, _, err := svc.ListProjects(context.Background(), agent, 0, ""); err != nil || len(list) != 1 {
		t.Fatalf("ListProjects: len=%d err=%v", len(list), err)
	}

	var forbidden *service.ForbiddenError
	if _, err := svc.CreateProject(context.Background(), agent, service.ProjectInput{Slug: "otro", Name: "Otro"}); !errors.As(err, &forbidden) {
		t.Fatalf("un agente no debía crear proyectos: %v", err)
	}
	if _, err := svc.SetMember(context.Background(), agent, project.Project.ID, human.Participant.ID, domain.PermissionReader, nil); !errors.As(err, &forbidden) {
		t.Fatalf("un lector no debía cambiar membresías: %v", err)
	}

	other, err := svc.CreateProject(context.Background(), human, service.ProjectInput{Slug: "proyecto-dos", Name: "Proyecto dos"})
	if err != nil {
		t.Fatalf("CreateProject dos: %v", err)
	}
	if _, err := svc.GetProject(context.Background(), agent, other.Project.ID); !errors.As(err, &forbidden) {
		t.Fatalf("el agente no debía leer un proyecto ajeno: %v", err)
	}

}

func TestMembershipChangeUpdatesAccessAndAuditsProjectChain(t *testing.T) {
	svc, _, database := newTestService(t)
	human, _ := bootstrap(t, svc)
	project, err := svc.CreateProject(context.Background(), human, service.ProjectInput{Slug: "membresias", Name: "Membresías"})
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	created, err := svc.CreateParticipant(context.Background(), human, service.ParticipantInput{
		Name: "agente-admin", Type: domain.ParticipantAgent,
		Projects: []service.ParticipantProjectInput{{ProjectID: project.Project.ID, Permission: domain.PermissionAdmin}},
	})
	if err != nil {
		t.Fatalf("CreateParticipant: %v", err)
	}
	agent, err := svc.Authenticate(context.Background(), created.Token)
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	member, err := svc.SetMember(context.Background(), agent, project.Project.ID, human.Participant.ID, domain.PermissionWriter, []string{"revisor"})
	if err != nil {
		t.Fatalf("SetMember: %v", err)
	}
	if member.Permission != domain.PermissionWriter || len(member.Roles) != 1 {
		t.Fatalf("miembro inesperado: %+v", member)
	}
	if err := svc.RemoveMember(context.Background(), agent, project.Project.ID, human.Participant.ID); err != nil {
		t.Fatalf("RemoveMember: %v", err)
	}
	verify, err := audit.Verify(context.Background(), database, project.Project.ID)
	if err != nil || !verify.OK {
		t.Fatalf("cadena del proyecto inválida: result=%+v err=%v", verify, err)
	}
	var actions int
	if err := database.QueryRow(`
		SELECT COUNT(1) FROM audit_events
		WHERE chain_id = ? AND action IN ('membresia.cambiar', 'membresia.quitar')`,
		project.Project.ID,
	).Scan(&actions); err != nil {
		t.Fatalf("contar acciones: %v", err)
	}
	if actions < 3 {
		t.Fatalf("acciones de membresía=%d, want >=3", actions)
	}
}

func TestAuditFailureRollsBackBusinessChange(t *testing.T) {
	svc, _, database := newTestService(t)
	human, _ := bootstrap(t, svc)
	if _, err := database.Exec(`UPDATE audit_chain_heads SET last_seq = 'no-es-un-numero' WHERE chain_id = 'system'`); err != nil {
		t.Fatalf("romper cabeza de cadena: %v", err)
	}
	if _, err := svc.CreateProject(context.Background(), human, service.ProjectInput{Slug: "rollback", Name: "Rollback"}); err == nil {
		t.Fatal("CreateProject debía fallar si falla la auditoría")
	}
	var projects, memberships int
	if err := database.QueryRow(`SELECT COUNT(1) FROM projects`).Scan(&projects); err != nil {
		t.Fatalf("contar proyectos: %v", err)
	}
	if err := database.QueryRow(`SELECT COUNT(1) FROM memberships`).Scan(&memberships); err != nil {
		t.Fatalf("contar membresías: %v", err)
	}
	if projects != 0 || memberships != 0 {
		t.Fatalf("el cambio no se revirtió: projects=%d memberships=%d", projects, memberships)
	}
}

func TestVerifyDetectsTamperedEvent(t *testing.T) {
	svc, _, database := newTestService(t)
	human, _ := bootstrap(t, svc)
	project, err := svc.CreateProject(context.Background(), human, service.ProjectInput{Slug: "auditoria", Name: "Auditoría"})
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	verify, err := audit.Verify(context.Background(), database, project.Project.ID)
	if err != nil || !verify.OK {
		t.Fatalf("cadena inicial inválida: %+v err=%v", verify, err)
	}
	if _, err := database.Exec(`
		UPDATE audit_events SET payload = '{"tampered":true}'
		WHERE chain_id = ? AND seq = 1`, project.Project.ID); err != nil {
		t.Fatalf("alterar evento: %v", err)
	}
	verify, err = audit.Verify(context.Background(), database, project.Project.ID)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if verify.OK || verify.FirstError == nil || verify.FirstError.Seq != 1 {
		t.Fatalf("no se detectó la alteración: %+v", verify)
	}
}

func TestAuditListAndChainsRespectProjectAccess(t *testing.T) {
	svc, _, _ := newTestService(t)
	human, _ := bootstrap(t, svc)
	project, err := svc.CreateProject(context.Background(), human, service.ProjectInput{Slug: "audit-access", Name: "Audit access"})
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	created, err := svc.CreateParticipant(context.Background(), human, service.ParticipantInput{
		Name: "agente-audit", Type: domain.ParticipantAgent,
		Projects: []service.ParticipantProjectInput{{ProjectID: project.Project.ID, Permission: domain.PermissionReader}},
	})
	if err != nil {
		t.Fatalf("CreateParticipant: %v", err)
	}
	agent, err := svc.Authenticate(context.Background(), created.Token)
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	events, next, err := svc.ListAudit(context.Background(), agent, service.AuditFilter{ChainID: project.Project.ID}, 0, "")
	if err != nil {
		t.Fatalf("ListAudit: %v", err)
	}
	if len(events) == 0 || next != "" {
		t.Fatalf("eventos=%d next=%q", len(events), next)
	}
	chains, _, err := svc.ListChains(context.Background(), agent, 0, "")
	if err != nil {
		t.Fatalf("ListChains: %v", err)
	}
	if len(chains) != 1 || chains[0].ChainID != project.Project.ID {
		t.Fatalf("cadenas inesperadas: %+v", chains)
	}
	var forbidden *service.ForbiddenError
	if _, _, err := svc.ListAudit(context.Background(), agent, service.AuditFilter{ChainID: domain.SystemChain}, 0, ""); !errors.As(err, &forbidden) {
		t.Fatalf("un agente no debía leer la cadena system: %v", err)
	}
	if _, err := svc.VerifyChain(context.Background(), agent, project.Project.ID); err != nil {
		t.Fatalf("VerifyChain: %v", err)
	}
}
