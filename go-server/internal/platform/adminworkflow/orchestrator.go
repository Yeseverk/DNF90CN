package adminworkflow

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"longheng.io/server/internal/platform/admincmd"
	"longheng.io/server/internal/platform/audit"
)

var (
	ErrServiceRequired = errors.New("admin workflow service is required")
	ErrCatalogRequired = errors.New("admin workflow catalog is required")
	ErrRBACRequired    = errors.New("admin workflow RBAC checker is required")
	ErrAuditRequired   = errors.New("admin workflow audit logger is required")
	ErrRBACDenied      = errors.New("admin workflow RBAC denied")
)

const (
	ActionDryRun         = "dry_run"
	ActionSubmitApproval = "submit_approval"
	ActionApprove        = "approve"
	ActionReject         = "reject"
	ActionExecute        = "execute"
)

type RBACChecker interface {
	CheckAdminWorkflow(context.Context, WorkflowTemplate, admincmd.Command, string, string) error
}

type RBACFunc func(context.Context, WorkflowTemplate, admincmd.Command, string, string) error

func (fn RBACFunc) CheckAdminWorkflow(ctx context.Context, template WorkflowTemplate, command admincmd.Command, action, actor string) error {
	if fn == nil {
		return ErrRBACRequired
	}
	return fn(ctx, template, command, action, actor)
}

type OrchestratorOptions struct {
	Service *Service
	Catalog Catalog
	RBAC    RBACChecker
	Audit   *audit.Logger
	Now     func() time.Time
}

type Orchestrator struct {
	service *Service
	catalog Catalog
	rbac    RBACChecker
	audit   *audit.Logger
	now     func() time.Time
}

type BeginRequest struct {
	Workflow       string
	Command        admincmd.Command
	Previewer      Previewer
	SubmitApproval bool
}

type DecisionRequest struct {
	ID       string
	Decision admincmd.ApprovalDecision
}

type ExecuteRequest struct {
	ID       string
	Actor    string
	Executor Executor
}

func NewOrchestrator(options OrchestratorOptions) (*Orchestrator, error) {
	if options.Service == nil {
		return nil, ErrServiceRequired
	}
	if len(options.Catalog.Workflows) == 0 {
		return nil, ErrCatalogRequired
	}
	if options.RBAC == nil {
		return nil, ErrRBACRequired
	}
	if options.Audit == nil {
		return nil, ErrAuditRequired
	}
	if err := ValidateCatalog(options.Catalog, CatalogValidateOptions{RequiredNames: WorkflowNames()}); err != nil {
		return nil, err
	}
	now := options.Now
	if now == nil {
		now = time.Now
	}
	return &Orchestrator{
		service: options.Service,
		catalog: NormalizeCatalog(options.Catalog),
		rbac:    options.RBAC,
		audit:   options.Audit,
		now:     now,
	}, nil
}

func (o *Orchestrator) Begin(ctx context.Context, request BeginRequest) (StartResult, error) {
	if err := ctxErr(ctx); err != nil {
		return StartResult{}, err
	}
	template, command, err := o.prepare(ctx, request.Workflow, request.Command, ActionDryRun, request.Command.Actor)
	if err != nil {
		return StartResult{}, err
	}
	if request.Previewer == nil {
		return StartResult{}, ErrPreviewerRequired
	}
	if err := o.recordAudit(ctx, ActionDryRun+".requested", template, command, command.Actor, nil, ""); err != nil {
		return StartResult{}, err
	}
	result, err := o.service.StartDryRun(ctx, command, request.Previewer)
	if err != nil {
		_ = o.recordAudit(ctx, ActionDryRun+".failed", template, command, command.Actor, nil, err.Error())
		return StartResult{}, err
	}
	record := result.Record
	record.Workflow = template.Name
	record.UpdatedAt = o.now().UTC()
	record, err = o.service.store.SaveWorkflow(ctx, record)
	if err != nil {
		return StartResult{}, err
	}
	result.Record = record
	if err := o.recordAudit(ctx, ActionDryRun+".succeeded", template, command, command.Actor, &record, ""); err != nil {
		return StartResult{}, err
	}
	if !request.SubmitApproval {
		return result, nil
	}
	record, err = o.SubmitApproval(ctx, record.ID)
	if err != nil {
		return StartResult{}, err
	}
	result.Record = record
	return result, nil
}

func (o *Orchestrator) SubmitApproval(ctx context.Context, id string) (Record, error) {
	record, template, err := o.recordForAction(ctx, id, ActionSubmitApproval, "")
	if err != nil {
		return Record{}, err
	}
	if err := o.recordAudit(ctx, ActionSubmitApproval+".requested", template, record.Command, record.Command.Actor, &record, ""); err != nil {
		return Record{}, err
	}
	record, err = o.service.SubmitApproval(ctx, id)
	if err != nil {
		_ = o.recordAudit(ctx, ActionSubmitApproval+".failed", template, record.Command, record.Command.Actor, &record, err.Error())
		return Record{}, err
	}
	record.Workflow = template.Name
	record, err = o.service.store.SaveWorkflow(ctx, record)
	if err != nil {
		return Record{}, err
	}
	if err := o.recordAudit(ctx, ActionSubmitApproval+".succeeded", template, record.Command, record.Command.Actor, &record, ""); err != nil {
		return Record{}, err
	}
	return record, nil
}

func (o *Orchestrator) Approve(ctx context.Context, request DecisionRequest) (Record, error) {
	record, template, err := o.recordForAction(ctx, request.ID, ActionApprove, request.Decision.Approver)
	if err != nil {
		return Record{}, err
	}
	if err := o.recordAudit(ctx, ActionApprove+".requested", template, record.Command, request.Decision.Approver, &record, ""); err != nil {
		return Record{}, err
	}
	record, err = o.service.Approve(ctx, request.ID, request.Decision)
	if err != nil {
		_ = o.recordAudit(ctx, ActionApprove+".failed", template, record.Command, request.Decision.Approver, &record, err.Error())
		return Record{}, err
	}
	record.Workflow = template.Name
	record, err = o.service.store.SaveWorkflow(ctx, record)
	if err != nil {
		return Record{}, err
	}
	if err := o.recordAudit(ctx, ActionApprove+".succeeded", template, record.Command, request.Decision.Approver, &record, ""); err != nil {
		return Record{}, err
	}
	return record, nil
}

func (o *Orchestrator) Reject(ctx context.Context, request DecisionRequest) (Record, error) {
	record, template, err := o.recordForAction(ctx, request.ID, ActionReject, request.Decision.Approver)
	if err != nil {
		return Record{}, err
	}
	if err := o.recordAudit(ctx, ActionReject+".requested", template, record.Command, request.Decision.Approver, &record, ""); err != nil {
		return Record{}, err
	}
	record, err = o.service.Reject(ctx, request.ID, request.Decision)
	if err != nil {
		_ = o.recordAudit(ctx, ActionReject+".failed", template, record.Command, request.Decision.Approver, &record, err.Error())
		return Record{}, err
	}
	record.Workflow = template.Name
	record, err = o.service.store.SaveWorkflow(ctx, record)
	if err != nil {
		return Record{}, err
	}
	if err := o.recordAudit(ctx, ActionReject+".succeeded", template, record.Command, request.Decision.Approver, &record, ""); err != nil {
		return Record{}, err
	}
	return record, nil
}

func (o *Orchestrator) Execute(ctx context.Context, request ExecuteRequest) (Record, error) {
	record, template, err := o.recordForAction(ctx, request.ID, ActionExecute, request.Actor)
	if err != nil {
		return Record{}, err
	}
	if request.Executor == nil {
		return Record{}, ErrExecutorRequired
	}
	if err := o.recordAudit(ctx, ActionExecute+".requested", template, record.Command, request.Actor, &record, ""); err != nil {
		return Record{}, err
	}
	record, err = o.service.Execute(ctx, request.ID, request.Executor)
	if err != nil {
		if latest, ok, getErr := o.service.Get(ctx, request.ID); getErr == nil && ok {
			record = latest
		}
		_ = o.recordAudit(ctx, ActionExecute+".failed", template, record.Command, request.Actor, &record, err.Error())
		return Record{}, err
	}
	record.Workflow = template.Name
	record, err = o.service.store.SaveWorkflow(ctx, record)
	if err != nil {
		return Record{}, err
	}
	if err := o.recordAudit(ctx, ActionExecute+".succeeded", template, record.Command, request.Actor, &record, ""); err != nil {
		return Record{}, err
	}
	return record, nil
}

func (o *Orchestrator) prepare(ctx context.Context, workflow string, command admincmd.Command, action, actor string) (WorkflowTemplate, admincmd.Command, error) {
	if o == nil || o.service == nil {
		return WorkflowTemplate{}, admincmd.Command{}, ErrServiceRequired
	}
	command = admincmd.Normalize(command)
	template, err := o.templateFor(workflow, command)
	if err != nil {
		return WorkflowTemplate{}, admincmd.Command{}, err
	}
	if err := o.rbac.CheckAdminWorkflow(ctx, template, command, action, strings.TrimSpace(actor)); err != nil {
		return WorkflowTemplate{}, admincmd.Command{}, fmt.Errorf("%w: %w", ErrRBACDenied, err)
	}
	return template, command, nil
}

func (o *Orchestrator) recordForAction(ctx context.Context, id, action, actor string) (Record, WorkflowTemplate, error) {
	if o == nil || o.service == nil {
		return Record{}, WorkflowTemplate{}, ErrServiceRequired
	}
	record, err := o.service.mustGet(ctx, id)
	if err != nil {
		return Record{}, WorkflowTemplate{}, err
	}
	if actor == "" {
		actor = record.Command.Actor
	}
	template, command, err := o.prepare(ctx, record.Workflow, record.Command, action, actor)
	if err != nil {
		return Record{}, WorkflowTemplate{}, err
	}
	record.Command = command
	if record.Workflow == "" {
		record.Workflow = template.Name
	}
	return record, template, nil
}

func (o *Orchestrator) templateFor(workflow string, command admincmd.Command) (WorkflowTemplate, error) {
	if o == nil || len(o.catalog.Workflows) == 0 {
		return WorkflowTemplate{}, ErrCatalogRequired
	}
	names := []string{workflow}
	if command.Operation != "" {
		names = append(names, strings.ReplaceAll(command.Operation, ".", "_"))
	}
	if command.Scope != "" {
		names = append(names, command.Scope)
	}
	for _, name := range names {
		if template, ok := o.catalog.FindWorkflow(name); ok {
			return template, nil
		}
	}
	return WorkflowTemplate{}, ErrWorkflowNotFound
}

func (o *Orchestrator) recordAudit(ctx context.Context, action string, template WorkflowTemplate, command admincmd.Command, actor string, record *Record, errText string) error {
	if o == nil || o.audit == nil {
		return ErrAuditRequired
	}
	fields := map[string]string{
		"workflow":        template.Name,
		"operation":       command.Operation,
		"scope":           command.Scope,
		"environment":     command.Environment,
		"shard_id":        command.ShardID,
		"idempotency_key": command.IdempotencyKey,
	}
	if record != nil {
		fields["workflow_id"] = record.ID
		fields["phase"] = string(record.Phase)
		if record.Receipt != nil {
			fields["receipt_id"] = record.Receipt.ID
			fields["receipt_status"] = record.Receipt.Status
		}
	}
	if errText != "" {
		fields["error"] = errText
	}
	hash, err := admincmd.ParamsHash(command.Params)
	if err != nil {
		return err
	}
	if hash != "" {
		fields["params_hash"] = hash
	}
	actor = strings.TrimSpace(actor)
	if actor == "" {
		actor = command.Actor
	}
	return o.audit.Record(ctx, audit.Event{
		Time:   o.now().UTC(),
		Actor:  actor,
		Action: action,
		Target: command.Target,
		Fields: fields,
	})
}
