package servergroup

import (
	"fmt"
	"sort"
)

// MergeModuleRecipe 描述项目侧实现一个合服模块时必须准备的输入、阶段动作、证据和验收项。
type MergeModuleRecipe struct {
	// Name 是模块名，和 MergeModuleTemplate、MergeModuleSpec 对齐。
	Name string `json:"name"`
	// Description 是模块职责说明。
	Description string `json:"description,omitempty"`
	// Features 是模块覆盖的合服 feature。
	Features []string `json:"features,omitempty"`
	// Required 表示正式项目是否必须实现该模块。
	Required bool `json:"required"`
	// Inputs 是实现该模块前必须能读取或生成的数据输入。
	Inputs []string `json:"inputs,omitempty"`
	// Phases 描述 gendb、merge、rollback 三个阶段的实现要求。
	Phases []MergePhaseRecipe `json:"phases,omitempty"`
	// Acceptance 是上线前验收该模块实现时必须检查的结果。
	Acceptance []string `json:"acceptance,omitempty"`
	// Notes 是接入时容易漏掉的工程注意事项。
	Notes []string `json:"notes,omitempty"`
}

// MergePhaseRecipe 描述单个阶段的目标、步骤、证据和失败策略。
type MergePhaseRecipe struct {
	// Phase 是阶段名，取值为 gendb、merge 或 rollback。
	Phase string `json:"phase"`
	// Goal 是该阶段要达成的目标。
	Goal string `json:"goal,omitempty"`
	// Steps 是项目侧处理器应该完成的主要步骤。
	Steps []string `json:"steps,omitempty"`
	// Evidence 是该阶段必须输出到 MergeModuleResult.Evidence 或归档系统的证据。
	Evidence []string `json:"evidence,omitempty"`
	// FailurePolicy 描述失败时应该返回 error 还是 blocker finding。
	FailurePolicy string `json:"failure_policy,omitempty"`
}

type mergeRecipeDetail struct {
	Inputs     []string
	GenDB      []string
	Merge      []string
	Rollback   []string
	Acceptance []string
	Notes      []string
}

// MergeModuleRecipes 返回从成熟项目合服模块经验抽出的默认实现配方，调用方可以安全修改返回值。
func MergeModuleRecipes() []MergeModuleRecipe {
	templates := MergeModuleTemplates()
	recipes := make([]MergeModuleRecipe, 0, len(templates))
	for _, template := range templates {
		recipe, err := MergeRecipeFromTemplate(template)
		if err != nil {
			continue
		}
		recipes = append(recipes, recipe)
	}
	return cloneMergeRecipes(recipes)
}

// FindMergeRecipe 按模块名查找默认合服模块实现配方。
func FindMergeRecipe(name string) (MergeModuleRecipe, bool) {
	name = normalizeID(name)
	if name == "" {
		return MergeModuleRecipe{}, false
	}
	for _, template := range defModuleTpls {
		if template.Name != name {
			continue
		}
		recipe, err := MergeRecipeFromTemplate(template)
		if err != nil {
			return MergeModuleRecipe{}, false
		}
		return cloneMergeRecipe(recipe), true
	}
	return MergeModuleRecipe{}, false
}

// MergeRecipeFromTemplate 把默认模板转换成项目侧实现配方。
func MergeRecipeFromTemplate(template MergeModuleTemplate) (MergeModuleRecipe, error) {
	template, err := normModuleTpl(template)
	if err != nil {
		return MergeModuleRecipe{}, err
	}
	detail, ok := defaultRecipeDetails[template.Name]
	if !ok {
		detail = genericRecipeDetail(template)
	}
	recipe := MergeModuleRecipe{
		Name:        template.Name,
		Description: template.Description,
		Features:    append([]string(nil), template.Features...),
		Required:    template.Required,
		Inputs:      detail.Inputs,
		Phases: []MergePhaseRecipe{
			{
				Phase:         MergeModulePhaseGenDB,
				Goal:          "生成合服前置数据、冲突方案和 dry-run 证据",
				Steps:         append(cloneMergeStrings(detail.GenDB), template.Checks...),
				Evidence:      append(cloneMergeStrings(template.Evidence), "gendb 阶段批次号", "抽样校验输入"),
				FailurePolicy: "可修复风险返回 blocker finding；读取失败、生成失败或证据落盘失败返回 error",
			},
			{
				Phase:         MergeModulePhaseMerge,
				Goal:          "在审批后的合服窗口内执行幂等业务合并",
				Steps:         detail.Merge,
				Evidence:      append(cloneMergeStrings(template.Evidence), "merge 阶段批次号", "写入数量和抽样校验摘要"),
				FailurePolicy: "业务写入失败必须返回 error；不能把半完成写入伪装成 warning",
			},
			{
				Phase:         MergeModulePhaseRollback,
				Goal:          "按归档和回滚点执行业务回滚或补偿",
				Steps:         append(cloneMergeStrings(detail.Rollback), template.Rollback...),
				Evidence:      append(cloneMergeStrings(template.Rollback), "rollback 阶段批次号", "恢复后抽样校验摘要"),
				FailurePolicy: "回滚失败必须返回 error，并保留可人工继续处理的证据引用",
			},
		},
		Acceptance: detail.Acceptance,
		Notes:      detail.Notes,
	}
	return normMergeRecipe(recipe)
}

// ValidateMergeModuleRecipe 校验项目侧自定义配方是否包含三段式实现要求。
func ValidateMergeModuleRecipe(recipe MergeModuleRecipe) error {
	_, err := normMergeRecipe(recipe)
	return err
}

var defaultRecipeDetails = map[string]mergeRecipeDetail{
	MergeModuleNameProfile: {
		Inputs:     []string{"Profile 快照", "角色名唯一索引", "账号绑定关系", "玩家镜像索引", "清理白名单"},
		GenDB:      []string{"批量读取源服和目标服玩家档案", "生成角色重名改名方案", "计算账号清理候选并套用白名单", "生成玩家镜像重建计划"},
		Merge:      []string{"按幂等批次写入目标 Profile", "重建角色名唯一索引", "重建账号到玩家镜像", "记录清理或保留结果"},
		Rollback:   []string{"读取 Profile 回滚快照", "恢复角色名唯一索引", "恢复账号到玩家镜像", "撤销误清理账号"},
		Acceptance: []string{"角色名索引全局唯一", "账号绑定能定位到主服玩家", "抽样 Profile 可读取且版本正确", "清理名单和保留名单可审计"},
		Notes:      []string{"名字冲突必须在 gendb 阶段生成确定方案", "清理策略必须可配置并写入证据"},
	},
	MergeModuleNameMail: {
		Inputs:     []string{"个人邮件快照", "全服邮件快照", "附件发放记录", "补偿邮件批次"},
		GenDB:      []string{"统计未领取邮件和附件", "生成全服邮件保留策略", "生成补偿邮件去重键", "冻结合服窗口新增邮件策略"},
		Merge:      []string{"按批次迁移个人邮件", "保留或重建全服邮件可见关系", "校验附件发放幂等记录", "写入补偿邮件批次"},
		Rollback:   []string{"删除合服批次新增邮件", "恢复旧服邮件索引", "冻结异常附件领取"},
		Acceptance: []string{"未领取邮件不丢失", "附件不可重复领取", "补偿邮件幂等键可复核", "全服邮件可见范围正确"},
		Notes:      []string{"邮件附件属于关键写路径，不能绕过幂等和发放 owner"},
	},
	MergeModuleNameLeaderboard: {
		Inputs:     []string{"本地榜快照", "跨服榜快照", "赛季窗口", "排行榜补充信息"},
		GenDB:      []string{"冻结或确认赛季结算窗口", "生成榜单重算计划", "检查同玩家多榜去重策略", "生成榜单补充信息校验集合"},
		Merge:      []string{"按赛季和榜单维度重算排名", "写入榜单补充信息", "记录榜单重算批次", "抽样校验排序稳定性"},
		Rollback:   []string{"恢复旧榜快照", "撤销新榜重算批次", "恢复榜单补充信息"},
		Acceptance: []string{"榜单排序稳定", "同玩家去重策略生效", "赛季结算窗口无交叉写入", "抽样榜单和补充信息一致"},
		Notes:      []string{"排行榜重算应输出 before/after 摘要，便于运营复核"},
	},
	MergeModuleNameGuild: {
		Inputs:     []string{"公会快照", "公会名唯一索引", "公会成员关系", "公会镜像", "公会榜快照"},
		GenDB:      []string{"生成公会重名改名方案", "校验会长和成员关系", "生成公会镜像重建计划", "确认公会活动结算窗口"},
		Merge:      []string{"迁移公会主体和成员关系", "重建公会名唯一索引", "重建公会镜像", "重算公会榜"},
		Rollback:   []string{"恢复公会主体快照", "恢复公会名索引", "恢复公会镜像", "撤销公会榜重算"},
		Acceptance: []string{"公会名索引唯一", "会长和成员关系完整", "公会镜像可查询", "公会榜抽样正确"},
		Notes:      []string{"公会成员关系和公会榜必须同批次留证，避免只迁主表"},
	},
	MergeModuleNameFriend: {
		Inputs:     []string{"好友双向边快照", "黑名单快照", "最近联系人", "好友活动记录"},
		GenDB:      []string{"检查好友双向边一致性", "确认黑名单优先级", "生成好友活动合并计划", "识别跨服好友引用"},
		Merge:      []string{"合并好友图谱", "合并黑名单和最近联系人", "迁移好友活动记录", "抽样校验双向边"},
		Rollback:   []string{"恢复好友图谱快照", "恢复黑名单和最近联系人", "撤销好友活动批次"},
		Acceptance: []string{"好友双向边一致", "黑名单优先级正确", "好友活动记录可追踪", "跨服引用有明确处理策略"},
		Notes:      []string{"好友模块应避免只写单向边"},
	},
	MergeModuleNameAuction: {
		Inputs:     []string{"拍卖订单", "竞价记录", "流拍记录", "战区归属"},
		GenDB:      []string{"冻结拍卖结算窗口", "校验竞价订单幂等键", "生成流拍记录去重计划", "确认战区归属变更"},
		Merge:      []string{"迁移未结算拍卖", "迁移竞价和流拍记录", "重建战区拍卖归属", "记录结算冻结解除点"},
		Rollback:   []string{"恢复拍卖快照", "撤销合服后竞价写入", "恢复战区拍卖归属"},
		Acceptance: []string{"拍卖结算窗口无并发写入", "竞价订单不重复", "流拍记录去重正确", "战区归属可复核"},
		Notes:      []string{"拍卖和支付订单交叉时必须明确 owner"},
	},
	MergeModuleNameActivity: {
		Inputs:     []string{"活动状态", "活动榜", "奖励领取记录", "活动开放窗口"},
		GenDB:      []string{"确认活动开放和结算窗口", "生成活动状态去重计划", "生成活动榜重算计划", "检查奖励领取幂等记录"},
		Merge:      []string{"合并活动状态", "重算活动榜", "合并奖励领取记录", "记录活动批次和窗口"},
		Rollback:   []string{"恢复活动状态快照", "撤销活动榜重算", "冻结异常奖励领取"},
		Acceptance: []string{"活动窗口正确", "活动状态无重复", "奖励领取不重复", "活动榜抽样正确"},
		Notes:      []string{"活动模块必须区分已结算、结算中和未开启状态"},
	},
	MergeModuleNamePayment: {
		Inputs:     []string{"支付订单", "IAP 订单", "发货记录", "补单复核单"},
		GenDB:      []string{"检查订单唯一键冲突", "统计已发货和未完成订单", "冻结合服窗口补单", "生成补单复核清单"},
		Merge:      []string{"迁移订单索引", "迁移发货幂等记录", "保留未完成订单冻结状态", "输出补单复核结果"},
		Rollback:   []string{"恢复订单索引", "冻结合服窗口补单", "恢复发货幂等记录"},
		Acceptance: []string{"订单唯一键不冲突", "已发货订单不可重复发货", "未完成订单状态可复核", "补单清单可人工追踪"},
		Notes:      []string{"支付模块失败必须走 error 和人工复核，不能只给 warning"},
	},
	MergeModuleNameServerInfo: {
		Inputs:     []string{"来源区服记录", "目标区服版本", "路由元数据", "合服批次号"},
		GenDB:      []string{"校验来源区服和主服关系", "生成合服批次号", "检查路由元数据一致性", "生成服务端公告和查询映射"},
		Merge:      []string{"写入合服批次记录", "写入来源区服映射", "更新路由元数据", "输出合服完成摘要"},
		Rollback:   []string{"恢复合服信息快照", "恢复旧路由元数据", "标记合服批次回滚"},
		Acceptance: []string{"来源区服能映射到主服", "路由元数据版本正确", "合服批次号幂等", "查询面能展示当前合服关系"},
		Notes:      []string{"合服信息是运营排障入口，必须和平台路由归档互相引用"},
	},
}

func genericRecipeDetail(template MergeModuleTemplate) mergeRecipeDetail {
	return mergeRecipeDetail{
		Inputs:     []string{template.Name + " 模块快照", template.Name + " 模块索引", "合服批次号"},
		GenDB:      []string{"读取模块快照", "生成冲突清单", "生成抽样校验集合"},
		Merge:      []string{"按幂等批次写入目标模块", "重建模块索引", "输出写入摘要"},
		Rollback:   []string{"读取模块回滚快照", "恢复模块索引", "输出恢复摘要"},
		Acceptance: []string{"模块索引可查询", "抽样数据一致", "批次号可审计"},
		Notes:      []string{"自定义模块必须声明 feature，并接入 gendb、merge、rollback 三段处理器"},
	}
}

func normMergeRecipe(recipe MergeModuleRecipe) (MergeModuleRecipe, error) {
	recipe.Name = normalizeID(recipe.Name)
	recipe.Description = firstNonEmpty(recipe.Description)
	recipe.Features = normalizeIDs(recipe.Features)
	recipe.Inputs = cloneMergeStrings(recipe.Inputs)
	recipe.Acceptance = cloneMergeStrings(recipe.Acceptance)
	recipe.Notes = cloneMergeStrings(recipe.Notes)
	if recipe.Name == "" {
		return MergeModuleRecipe{}, fmt.Errorf("%w: recipe name is required", ErrMergeModuleInvalid)
	}
	if len(recipe.Features) == 0 {
		return MergeModuleRecipe{}, fmt.Errorf("%w: recipe %s features are required", ErrMergeModuleInvalid, recipe.Name)
	}
	if len(recipe.Inputs) == 0 {
		return MergeModuleRecipe{}, fmt.Errorf("%w: recipe %s inputs are required", ErrMergeModuleInvalid, recipe.Name)
	}
	if len(recipe.Acceptance) == 0 {
		return MergeModuleRecipe{}, fmt.Errorf("%w: recipe %s acceptance items are required", ErrMergeModuleInvalid, recipe.Name)
	}
	phases, err := normPhaseRecipes(recipe.Phases)
	if err != nil {
		return MergeModuleRecipe{}, err
	}
	recipe.Phases = phases
	for _, phase := range []string{MergeModulePhaseGenDB, MergeModulePhaseMerge, MergeModulePhaseRollback} {
		if !recipeHasPhase(recipe.Phases, phase) {
			return MergeModuleRecipe{}, fmt.Errorf("%w: recipe %s missing %s phase", ErrMergeModuleInvalid, recipe.Name, phase)
		}
	}
	return recipe, nil
}

func normPhaseRecipes(phases []MergePhaseRecipe) ([]MergePhaseRecipe, error) {
	if len(phases) == 0 {
		return nil, fmt.Errorf("%w: recipe phases are required", ErrMergeModuleInvalid)
	}
	seen := make(map[string]struct{}, len(phases))
	out := make([]MergePhaseRecipe, 0, len(phases))
	for _, phase := range phases {
		phase.Phase = normalizeID(phase.Phase)
		phase.Goal = firstNonEmpty(phase.Goal)
		phase.Steps = cloneMergeStrings(phase.Steps)
		phase.Evidence = cloneMergeStrings(phase.Evidence)
		phase.FailurePolicy = firstNonEmpty(phase.FailurePolicy)
		if !validMergePhase(phase.Phase) {
			return nil, fmt.Errorf("%w: recipe phase %s is invalid", ErrMergeModuleInvalid, phase.Phase)
		}
		if _, exists := seen[phase.Phase]; exists {
			return nil, fmt.Errorf("%w: recipe phase %s duplicated", ErrMergeModuleInvalid, phase.Phase)
		}
		if phase.Goal == "" || len(phase.Steps) == 0 || len(phase.Evidence) == 0 || phase.FailurePolicy == "" {
			return nil, fmt.Errorf("%w: recipe phase %s is incomplete", ErrMergeModuleInvalid, phase.Phase)
		}
		seen[phase.Phase] = struct{}{}
		out = append(out, phase)
	}
	sort.Slice(out, func(i, j int) bool {
		return recipePhaseOrder(out[i].Phase) < recipePhaseOrder(out[j].Phase)
	})
	return out, nil
}

func recipeHasPhase(phases []MergePhaseRecipe, phase string) bool {
	phase = normalizeID(phase)
	for _, item := range phases {
		if item.Phase == phase {
			return true
		}
	}
	return false
}

func recipePhaseOrder(phase string) int {
	switch phase {
	case MergeModulePhaseGenDB:
		return 1
	case MergeModulePhaseMerge:
		return 2
	case MergeModulePhaseRollback:
		return 3
	default:
		return 100
	}
}

func cloneMergeRecipes(recipes []MergeModuleRecipe) []MergeModuleRecipe {
	if len(recipes) == 0 {
		return nil
	}
	out := make([]MergeModuleRecipe, len(recipes))
	for i, recipe := range recipes {
		out[i] = cloneMergeRecipe(recipe)
	}
	return out
}

func cloneMergeRecipe(recipe MergeModuleRecipe) MergeModuleRecipe {
	recipe.Features = append([]string(nil), recipe.Features...)
	recipe.Inputs = cloneMergeStrings(recipe.Inputs)
	recipe.Phases = clonePhaseRecipes(recipe.Phases)
	recipe.Acceptance = cloneMergeStrings(recipe.Acceptance)
	recipe.Notes = cloneMergeStrings(recipe.Notes)
	return recipe
}

func clonePhaseRecipes(phases []MergePhaseRecipe) []MergePhaseRecipe {
	if len(phases) == 0 {
		return nil
	}
	out := make([]MergePhaseRecipe, len(phases))
	for i, phase := range phases {
		out[i] = MergePhaseRecipe{
			Phase:         phase.Phase,
			Goal:          phase.Goal,
			Steps:         cloneMergeStrings(phase.Steps),
			Evidence:      cloneMergeStrings(phase.Evidence),
			FailurePolicy: phase.FailurePolicy,
		}
	}
	return out
}
