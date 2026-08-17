package adminworkflow

import (
	"errors"
	"fmt"
	"sort"

	"longheng.io/server/internal/platform/admincmd"
)

var ErrOperationRecipeInvalid = errors.New("admin operation recipe is invalid")

// OperationRecipe 描述一个可复用运营命令从参数、预览、执行到回滚证据的接入配方。
type OperationRecipe struct {
	// Name 是运营配方名，和 admincmd 模板、adminworkflow 工作流名对齐。
	Name string `json:"name"`
	// Description 是配方用途说明。
	Description string `json:"description,omitempty"`
	// Source 是抽取来源类别，通常标记为成熟项目GM 或龙恒平台能力。
	Source string `json:"source,omitempty"`
	// Command 是对应的 admincmd 模板。
	Command admincmd.Template `json:"command"`
	// Workflow 是对应的 Admin 工作流控制要求。
	Workflow WorkflowTemplate `json:"workflow"`
	// DryRunSteps 是执行前必须产出的预览或检查步骤。
	DryRunSteps []string `json:"dry_run_steps,omitempty"`
	// ExecuteSteps 是审批后真正执行的步骤。
	ExecuteSteps []string `json:"execute_steps,omitempty"`
	// RollbackSteps 是失败或误操作后的回滚、补偿或人工修复步骤。
	RollbackSteps []string `json:"rollback_steps,omitempty"`
	// Notes 是项目接入时容易遗漏的工程注意事项。
	Notes []string `json:"notes,omitempty"`
}

// DefaultOperationRecipes 返回从成熟项目GM 操作经验抽出的默认运营命令配方。
func DefaultOperationRecipes() []OperationRecipe {
	return cloneOpRecipes(defOpRecipes)
}

// FindDefaultOperationRecipe 按名称查找默认运营命令配方。
func FindDefaultOperationRecipe(name string) (OperationRecipe, bool) {
	name = normalizeWorkflowID(name)
	for _, recipe := range defOpRecipes {
		if recipe.Name == name {
			return cloneOperationRecipe(recipe), true
		}
	}
	return OperationRecipe{}, false
}

// ValidateOperationRecipe 校验运营命令配方是否具备命令模板、工作流控制、步骤和证据。
func ValidateOperationRecipe(recipe OperationRecipe) error {
	_, err := normOpRecipe(recipe)
	return err
}

// DefaultOperationCommandTemplates 返回默认运营命令配方中的 admincmd 模板。
func DefaultOperationCommandTemplates() ([]admincmd.Template, error) {
	recipes := DefaultOperationRecipes()
	out := make([]admincmd.Template, 0, len(recipes))
	for _, recipe := range recipes {
		normalized, err := normOpRecipe(recipe)
		if err != nil {
			return nil, err
		}
		out = append(out, normalized.Command)
	}
	return out, nil
}

// DefaultOperationWorkflowTemplates 返回默认运营命令配方中的 Admin 工作流模板。
func DefaultOperationWorkflowTemplates() ([]WorkflowTemplate, error) {
	recipes := DefaultOperationRecipes()
	out := make([]WorkflowTemplate, 0, len(recipes))
	for _, recipe := range recipes {
		normalized, err := normOpRecipe(recipe)
		if err != nil {
			return nil, err
		}
		out = append(out, normalized.Workflow)
	}
	return out, nil
}

var defOpRecipes = []OperationRecipe{
	{
		Name:        "player_query",
		Description: "玩家基础信息、会话和 Profile 版本查询",
		Source:      "成熟项目GM player/profile 查询经验",
		Command: admincmd.Template{
			Name:      "player_query",
			Operation: "player.query",
			Scope:     "logic",
			Target:    "player",
			Params: []admincmd.ParamSpec{
				{Name: "account_id", Type: admincmd.ParamString, Required: true},
				{Name: "shard_id", Type: admincmd.ParamString},
			},
		},
		Workflow: WorkflowTemplate{
			Name:             "player_query",
			RequiredControls: []string{"RBAC", "read_only", "audit", "trace_id"},
			Evidence:         []string{"player overview snapshot", "session snapshot", "profile version"},
		},
		DryRunSteps:   []string{"读取玩家在线状态", "读取 Profile 版本", "读取最近会话摘要"},
		ExecuteSteps:  []string{"返回只读查询结果", "记录审计查询"},
		RollbackSteps: []string{"只读操作不需要回滚"},
		Notes:         []string{"查询也要审计，避免后台成为隐形数据出口"},
	},
	{
		Name:        "ban_or_unban",
		Description: "封禁、解封、禁言和解除禁言",
		Source:      "成熟项目GM ban 操作经验",
		Command: admincmd.Template{
			Name:      "ban_or_unban",
			Operation: "moderation.sanction",
			Scope:     "moderation",
			Target:    "account",
			Dangerous: true,
			Params: []admincmd.ParamSpec{
				{Name: "account_id", Type: admincmd.ParamString, Required: true},
				{Name: "action", Type: admincmd.ParamString, Required: true, Allowed: []string{"ban", "unban", "mute", "unmute"}},
				{Name: "duration_seconds", Type: admincmd.ParamInt},
				{Name: "reason_code", Type: admincmd.ParamString, Required: true},
			},
		},
		Workflow: WorkflowTemplate{
			Name:             "ban_or_unban",
			RequiredControls: []string{"RBAC", "reason", "duration_or_policy", "dangerous_confirmation", "idempotency_key", "audit", "command_receipt"},
			Evidence:         []string{"moderation record", "gateway refresh", "audit query"},
		},
		DryRunSteps:   []string{"查询当前处罚状态", "校验处罚策略", "生成影响范围预览"},
		ExecuteSteps:  []string{"写入处罚记录", "刷新网关会话状态", "返回 command receipt"},
		RollbackSteps: []string{"按 receipt 撤销本次处罚变更", "刷新网关会话状态"},
		Notes:         []string{"封禁类操作必须有原因码和审计记录"},
	},
	{
		Name:        "mail_publish",
		Description: "单人邮件、全服邮件和运营公告发布",
		Source:      "成熟项目GM mail/public_notice 操作经验",
		Command: admincmd.Template{
			Name:      "mail_publish",
			Operation: "notice.publish",
			Scope:     "logic",
			Target:    "notice",
			Dangerous: true,
			Params: []admincmd.ParamSpec{
				{Name: "notice_id", Type: admincmd.ParamString, Required: true},
				{Name: "audience", Type: admincmd.ParamString, Required: true, Allowed: []string{"account", "shard", "all"}},
				{Name: "account_id", Type: admincmd.ParamString},
				{Name: "shard_id", Type: admincmd.ParamString},
			},
		},
		Workflow: WorkflowTemplate{
			Name:             "mail_publish",
			RequiredControls: []string{"dry_run", "RBAC", "dangerous_confirmation", "idempotency_key", "audit", "command_receipt", "rollback_note"},
			Evidence:         []string{"audience preview", "notice publish receipt", "delivery sample"},
		},
		DryRunSteps:   []string{"校验收件范围", "抽样展示邮件内容", "检查附件幂等键"},
		ExecuteSteps:  []string{"写入公告或邮件批次", "触发在线推送", "记录发放证据"},
		RollbackSteps: []string{"冻结未领取附件", "撤销未生效公告", "生成客服补偿清单"},
		Notes:         []string{"邮件附件必须接入可靠发放 owner，不能只写后台记录"},
	},
	{
		Name:        "profile_repair",
		Description: "玩家 Profile 检查、字段修复和批量修复",
		Source:      "成熟项目GM profile 修复经验",
		Command: admincmd.Template{
			Name:      "profile_repair",
			Operation: "profile.repair",
			Scope:     "logic",
			Target:    "player",
			Dangerous: true,
			Params: []admincmd.ParamSpec{
				{Name: "account_id", Type: admincmd.ParamString, Required: true},
				{Name: "field", Type: admincmd.ParamString, Required: true},
				{Name: "mode", Type: admincmd.ParamString, Required: true, Allowed: []string{"check", "repair"}},
				{Name: "expected_version", Type: admincmd.ParamString},
			},
		},
		Workflow: WorkflowTemplate{
			Name:             "profile_repair",
			RequiredControls: []string{"read_only_preview", "RBAC", "dangerous_confirmation", "idempotency_key", "audit", "command_receipt", "rollback_plan"},
			Evidence:         []string{"before profile snapshot", "admin command receipt", "EventLog entry", "after profile snapshot"},
		},
		DryRunSteps:   []string{"读取修复前 Profile 快照", "校验字段 owner", "生成字段差异预览"},
		ExecuteSteps:  []string{"通过 owner 写路径修复字段", "写入 EventLog", "返回修复 receipt"},
		RollbackSteps: []string{"按修复前快照生成回滚命令", "校验 Profile 版本后恢复"},
		Notes:         []string{"Profile 修复不能绕过 owner、幂等和 EventLog"},
	},
	{
		Name:        "hotdata_publish",
		Description: "热数据发布、检查和回滚",
		Source:      "成熟项目GM hotdata 操作经验",
		Command: admincmd.Template{
			Name:      "hotdata_publish",
			Operation: "hotdata.publish",
			Scope:     "config",
			Target:    "hotdata",
			Dangerous: true,
			Params: []admincmd.ParamSpec{
				{Name: "bundle_id", Type: admincmd.ParamString, Required: true},
				{Name: "version", Type: admincmd.ParamString, Required: true},
				{Name: "mode", Type: admincmd.ParamString, Required: true, Allowed: []string{"dry_run", "apply", "rollback"}},
			},
		},
		Workflow: WorkflowTemplate{
			Name:             "hotdata_publish",
			RequiredControls: []string{"dry_run", "RBAC", "dangerous_confirmation", "idempotency_key", "audit", "command_receipt", "rollback_note"},
			Evidence:         []string{"hotdata diff", "checksum report", "reload receipt"},
		},
		DryRunSteps:   []string{"校验 bundle checksum", "生成配置差异", "检查目标服务 reload 能力"},
		ExecuteSteps:  []string{"发布热数据版本", "触发受控 reload", "采集服务确认回执"},
		RollbackSteps: []string{"切回上一版本", "再次触发 reload", "验证配置 checksum"},
		Notes:         []string{"热数据发布必须有差异报告和回滚 intent"},
	},
	{
		Name:        "hotpatch_rollout",
		Description: "代码热补丁发布、灰度和回滚",
		Source:      "成熟项目GM hotpatch 操作经验",
		Command: admincmd.Template{
			Name:      "hotpatch_rollout",
			Operation: "hotpatch.rollout",
			Scope:     "runtime",
			Target:    "hotpatch",
			Dangerous: true,
			Params: []admincmd.ParamSpec{
				{Name: "patch_id", Type: admincmd.ParamString, Required: true},
				{Name: "action", Type: admincmd.ParamString, Required: true, Allowed: []string{"dry_run", "rollout", "rollback"}},
				{Name: "target_nodes", Type: admincmd.ParamString},
			},
		},
		Workflow: WorkflowTemplate{
			Name:             "hotpatch_rollout",
			RequiredControls: []string{"dry_run", "RBAC", "dangerous_confirmation", "idempotency_key", "audit", "command_receipt", "rollback_note", "health verification"},
			Evidence:         []string{"patch manifest", "target node list", "rollout receipt", "health after rollout"},
		},
		DryRunSteps:   []string{"校验补丁签名和目标节点", "生成影响范围", "确认回滚补丁存在"},
		ExecuteSteps:  []string{"按节点灰度发布补丁", "采集健康检查", "记录 rollout receipt"},
		RollbackSteps: []string{"按补丁 ID 回滚", "恢复节点健康", "输出回滚证据"},
		Notes:         []string{"热补丁必须先小流量灰度，不能直接全量覆盖"},
	},
	{
		Name:        "payment_replay",
		Description: "支付订单查询、补单、重发和发货回放",
		Source:      "成熟项目GM pay_opt 操作经验",
		Command: admincmd.Template{
			Name:      "payment_replay",
			Operation: "commerce.fulfillment.replay",
			Scope:     "commerce",
			Target:    "order",
			Dangerous: true,
			Params: []admincmd.ParamSpec{
				{Name: "order_id", Type: admincmd.ParamString, Required: true},
				{Name: "channel", Type: admincmd.ParamString, Required: true},
				{Name: "mode", Type: admincmd.ParamString, Required: true, Allowed: []string{"query", "replay", "compensate"}},
			},
		},
		Workflow: WorkflowTemplate{
			Name:             "payment_replay",
			RequiredControls: []string{"order inspect", "dry_run", "RBAC", "dangerous_confirmation", "idempotency_key", "audit", "command_receipt", "rollback_note"},
			Evidence:         []string{"payment order snapshot", "fulfillment idempotency record", "delivery receipt"},
		},
		DryRunSteps:   []string{"查询订单状态", "查询发货幂等记录", "判断是否允许补偿"},
		ExecuteSteps:  []string{"通过发货 owner 重放", "写入补单复核记录", "返回发货 receipt"},
		RollbackSteps: []string{"冻结异常发货", "生成客服人工处理单", "保留支付复核证据"},
		Notes:         []string{"支付补单不能绕过订单唯一键和发货幂等记录"},
	},
	{
		Name:        "servergroup_merge",
		Description: "区服合服 dry-run、执行、回滚和证据归档",
		Source:      "成熟项目GM merge 操作经验",
		Command: admincmd.Template{
			Name:      "servergroup_merge",
			Operation: "servergroup.merge.workflow",
			Scope:     "servergroup",
			Target:    "merge",
			Dangerous: true,
			Params: []admincmd.ParamSpec{
				{Name: "merge_id", Type: admincmd.ParamString, Required: true},
				{Name: "main_shard_id", Type: admincmd.ParamString, Required: true},
				{Name: "shards", Type: admincmd.ParamString, Required: true},
				{Name: "mode", Type: admincmd.ParamString, Required: true, Allowed: []string{"dry_run", "apply", "rollback_dry_run", "rollback"}},
				{Name: "check_features", Type: admincmd.ParamString},
				{Name: "block_features", Type: admincmd.ParamString},
				{Name: "rollback_archive_id", Type: admincmd.ParamString},
			},
		},
		Workflow: WorkflowTemplate{
			Name:             "servergroup_merge",
			RequiredControls: []string{"merge dry_run", "module report", "RBAC", "dangerous_confirmation", "idempotency_key", "audit", "command_receipt", "adminworkflow_adapter", "merge_workflow_runner", "merge_workflow_archive", "archive_store", "archive_query_api", "workflow_record_query_api", "rollback_note"},
			Evidence:         []string{"adminworkflow.DryRun", "adminworkflow.Record", "adminworkflow.RollbackNote", "MigrationPlan", "MergeArchive", "MergeModuleRunReport", "RollbackPoint"},
		},
		DryRunSteps:   []string{"生成平台 MigrationPlan", "执行模块 GenDB 报告", "保存 dry-run 归档"},
		ExecuteSteps:  []string{"校验审批和幂等键", "执行模块 Merge 报告", "应用平台路由并保存归档"},
		RollbackSteps: []string{"读取 apply 归档", "执行模块 Rollback 报告", "恢复平台 RollbackPoint"},
		Notes:         []string{"合服必须保留模块报告、平台回滚点和后台可查询归档"},
	},
}

func normOpRecipe(recipe OperationRecipe) (OperationRecipe, error) {
	recipe.Name = normalizeWorkflowID(recipe.Name)
	recipe.Command = normCmdTemplate(recipe.Command)
	recipe.Workflow = NormalizeWorkflowTemplate(recipe.Workflow)
	recipe.DryRunSteps = normWorkflowList(recipe.DryRunSteps)
	recipe.ExecuteSteps = normWorkflowList(recipe.ExecuteSteps)
	recipe.RollbackSteps = normWorkflowList(recipe.RollbackSteps)
	recipe.Notes = normWorkflowList(recipe.Notes)
	if recipe.Name == "" {
		return OperationRecipe{}, fmt.Errorf("%w: name is required", ErrOperationRecipeInvalid)
	}
	if recipe.Command.Name != recipe.Name {
		return OperationRecipe{}, fmt.Errorf("%w: command template name must match %s", ErrOperationRecipeInvalid, recipe.Name)
	}
	if recipe.Workflow.Name != recipe.Name {
		return OperationRecipe{}, fmt.Errorf("%w: workflow template name must match %s", ErrOperationRecipeInvalid, recipe.Name)
	}
	if len(recipe.DryRunSteps) == 0 || len(recipe.ExecuteSteps) == 0 || len(recipe.RollbackSteps) == 0 {
		return OperationRecipe{}, fmt.Errorf("%w: %s steps are required", ErrOperationRecipeInvalid, recipe.Name)
	}
	if err := ValidateWorkflowTemplate(recipe.Workflow); err != nil {
		return OperationRecipe{}, fmt.Errorf("%w: %w", ErrOperationRecipeInvalid, err)
	}
	if recipe.Command.Operation == "" || recipe.Command.Target == "" {
		return OperationRecipe{}, fmt.Errorf("%w: %s command operation and target are required", ErrOperationRecipeInvalid, recipe.Name)
	}
	if recipe.Command.Dangerous {
		controls := workflowSet(recipe.Workflow.RequiredControls)
		for _, required := range []string{"dangerous_confirmation", "idempotency_key", "command_receipt"} {
			if !hasWorkflowItem(controls, required) {
				return OperationRecipe{}, fmt.Errorf("%w: dangerous recipe %s missing %s", ErrOperationRecipeInvalid, recipe.Name, required)
			}
		}
	}
	return recipe, nil
}

func normCmdTemplate(template admincmd.Template) admincmd.Template {
	registry, err := admincmd.NewTemplateRegistry(template)
	if err != nil {
		return template
	}
	normalized, ok := registry.Get(template.Name)
	if !ok {
		return template
	}
	return normalized
}

func cloneOpRecipes(recipes []OperationRecipe) []OperationRecipe {
	if len(recipes) == 0 {
		return nil
	}
	out := make([]OperationRecipe, len(recipes))
	for index, recipe := range recipes {
		out[index] = cloneOperationRecipe(recipe)
	}
	return out
}

func cloneOperationRecipe(recipe OperationRecipe) OperationRecipe {
	recipe.Command.Params = append([]admincmd.ParamSpec(nil), recipe.Command.Params...)
	recipe.Workflow.RequiredControls = append([]string(nil), recipe.Workflow.RequiredControls...)
	recipe.Workflow.Evidence = append([]string(nil), recipe.Workflow.Evidence...)
	recipe.DryRunSteps = append([]string(nil), recipe.DryRunSteps...)
	recipe.ExecuteSteps = append([]string(nil), recipe.ExecuteSteps...)
	recipe.RollbackSteps = append([]string(nil), recipe.RollbackSteps...)
	recipe.Notes = append([]string(nil), recipe.Notes...)
	return recipe
}

func sortedOpNames(recipes []OperationRecipe) []string {
	names := make([]string, 0, len(recipes))
	for _, recipe := range recipes {
		names = append(names, recipe.Name)
	}
	sort.Strings(names)
	return names
}
