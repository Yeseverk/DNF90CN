package servergroup

import "context"

const (
	// MergeModuleNameProfile 是角色档案合服模块名。
	MergeModuleNameProfile = "profile"
	// MergeModuleNameMail 是邮件合服模块名。
	MergeModuleNameMail = "mail"
	// MergeModuleNameLeaderboard 是排行榜合服模块名。
	MergeModuleNameLeaderboard = "leaderboard"
	// MergeModuleNameGuild 是公会合服模块名。
	MergeModuleNameGuild = "guild"
	// MergeModuleNameFriend 是好友合服模块名。
	MergeModuleNameFriend = "friend"
	// MergeModuleNameAuction 是拍卖行合服模块名。
	MergeModuleNameAuction = "auction"
	// MergeModuleNameActivity 是活动合服模块名。
	MergeModuleNameActivity = "activity"
	// MergeModuleNamePayment 是支付合服模块名。
	MergeModuleNamePayment = "payment"
	// MergeModuleNameServerInfo 是服务器信息合服模块名。
	MergeModuleNameServerInfo = "server_merge_info"
)

const (
	// MergeFeatureProfile 是角色档案合服功能域。
	MergeFeatureProfile = "profile"
	// MergeFeatureRoleName 是角色名冲突检查功能域。
	MergeFeatureRoleName = "role_name"
	// MergeFeaturePlayerMirror 是角色镜像数据功能域。
	MergeFeaturePlayerMirror = "player_mirror"
	// MergeFeatureAccountClean 是账号清理功能域。
	MergeFeatureAccountClean = "account_clean"
	// MergeFeatureMail 是邮件合服功能域。
	MergeFeatureMail = "mail"
	// MergeFeatureGlobalMail 是全服邮件合服功能域。
	MergeFeatureGlobalMail = "global_mail"
	// MergeFeatureLeaderboard 是排行榜合服功能域。
	MergeFeatureLeaderboard = "leaderboard"
	// MergeFeatureRank 是通用排名功能域。
	MergeFeatureRank = "rank"
	// MergeFeatureLocalRank 是本服排名功能域。
	MergeFeatureLocalRank = "local_rank"
	// MergeFeatureCrossRank 是跨服排名功能域。
	MergeFeatureCrossRank = "cross_rank"
	// MergeFeatureGuild 是公会合服功能域。
	MergeFeatureGuild = "guild"
	// MergeFeatureGuildName 是公会名冲突检查功能域。
	MergeFeatureGuildName = "guild_name"
	// MergeFeatureGuildRank 是公会排行功能域。
	MergeFeatureGuildRank = "guild_rank"
	// MergeFeatureGuildMirror 是公会镜像数据功能域。
	MergeFeatureGuildMirror = "guild_mirror"
	// MergeFeatureFriend 是好友合服功能域。
	MergeFeatureFriend = "friend"
	// MergeFeatureFriendRecharge 是好友充值关系功能域。
	MergeFeatureFriendRecharge = "friend_recharge"
	// MergeFeatureAuction 是拍卖行合服功能域。
	MergeFeatureAuction = "auction"
	// MergeFeatureWarzoneAuction 是战区拍卖行功能域。
	MergeFeatureWarzoneAuction = "warzone_auction"
	// MergeFeatureActivity 是活动合服功能域。
	MergeFeatureActivity = "activity"
	// MergeFeatureActivityState 是活动状态功能域。
	MergeFeatureActivityState = "activity_state"
	// MergeFeatureActivityRank 是活动排行功能域。
	MergeFeatureActivityRank = "activity_rank"
	// MergeFeaturePayment 是支付合服功能域。
	MergeFeaturePayment = "payment"
	// MergeFeatureOrder 是订单合服功能域。
	MergeFeatureOrder = "order"
	// MergeFeatureIAP 是内购回执合服功能域。
	MergeFeatureIAP = "iap"
	// MergeFeatureServerGroup 是服务器分组路由功能域。
	MergeFeatureServerGroup = "servergroup"
	// MergeFeatureServerInfo 是服务器信息合服功能域。
	MergeFeatureServerInfo = "server_merge_info"
)

// MergeModuleHandlers 保存项目侧对一个默认合服模块的三段式处理器。
type MergeModuleHandlers struct {
	// GenDB 负责合服前生成预检数据、冲突方案和 dry-run 证据。
	GenDB MergeGenDBFunc
	// Merge 负责合服窗口内的真实业务数据合并。
	Merge MergeApplyFunc
	// Rollback 负责合服失败后的业务回滚或补偿。
	Rollback MergeRollbackFunc
}

// MergeModuleTemplate 描述从成熟项目合服对象中抽出的默认模块规格，不包含项目私有表结构。
type MergeModuleTemplate struct {
	// Name 是默认模块名，和 MergeModuleSpec.Name 保持一致。
	Name string `json:"name"`
	// Description 是模块职责说明，用于报告和运营后台展示。
	Description string `json:"description,omitempty"`
	// Features 是该模块覆盖的合服 feature 别名集合。
	Features []string `json:"features,omitempty"`
	// Required 表示正式项目接入时该模块是否必须提供三段处理器。
	Required bool `json:"required"`
	// Checks 是项目侧实现至少要输出或验证的检查项。
	Checks []string `json:"checks,omitempty"`
	// Evidence 是项目侧实现应归档的证据类型说明。
	Evidence []string `json:"evidence,omitempty"`
	// Rollback 是项目侧实现应准备的回滚动作说明。
	Rollback []string `json:"rollback,omitempty"`
}

// MergeModuleTemplates 返回龙恒默认合服模块模板，调用方可以安全修改返回值。
func MergeModuleTemplates() []MergeModuleTemplate {
	return cloneMergeTemplates(defModuleTpls)
}

// FindMergeTemplate 按模块名查找默认合服模板。
func FindMergeTemplate(name string) (MergeModuleTemplate, bool) {
	name = normalizeID(name)
	for _, template := range defModuleTpls {
		if template.Name == name {
			return cloneMergeTemplate(template), true
		}
	}
	return MergeModuleTemplate{}, false
}

// Spec 用项目侧处理器把模板转换为可注册的合服模块规格。
func (t MergeModuleTemplate) Spec(handlers MergeModuleHandlers) (MergeModuleSpec, error) {
	template, err := normModuleTpl(t)
	if err != nil {
		return MergeModuleSpec{}, err
	}
	return normMergeSpec(MergeModuleSpec{
		Name:        template.Name,
		Description: template.Description,
		Features:    template.Features,
		Required:    template.Required,
		GenDB:       handlers.GenDB,
		Merge:       handlers.Merge,
		Rollback:    handlers.Rollback,
	})
}

// MergeModuleSpecs 返回带占位处理器的默认模块规格，占位处理器会在请求命中对应 feature 时阻断计划。
func MergeModuleSpecs() ([]MergeModuleSpec, error) {
	templates := MergeModuleTemplates()
	specs := make([]MergeModuleSpec, 0, len(templates))
	for _, template := range templates {
		spec, err := tmplPlaceholderSpec(template)
		if err != nil {
			return nil, err
		}
		specs = append(specs, spec)
	}
	return specs, nil
}

// BuildMergeModuleSpecs 用项目侧实现覆盖默认占位模块，未覆盖模块仍会在请求命中时阻断。
func BuildMergeModuleSpecs(overrides ...MergeModuleSpec) ([]MergeModuleSpec, error) {
	defaults, err := MergeModuleSpecs()
	if err != nil {
		return nil, err
	}
	byName := make(map[string]MergeModuleSpec, len(defaults)+len(overrides))
	names := make([]string, 0, len(defaults)+len(overrides))
	for _, spec := range defaults {
		byName[spec.Name] = spec
		names = append(names, spec.Name)
	}
	for _, override := range overrides {
		spec, err := templateOverrideSpec(override)
		if err != nil {
			return nil, err
		}
		if _, exists := byName[spec.Name]; !exists {
			names = append(names, spec.Name)
		}
		byName[spec.Name] = spec
	}
	names = normalizeIDs(names)
	specs := make([]MergeModuleSpec, 0, len(names))
	for _, name := range names {
		specs = append(specs, byName[name])
	}
	return specs, nil
}

// NewDefaultMergeRegistry 创建带默认模板保护的注册器，适合新项目接入期防止遗漏模块。
func NewDefaultMergeRegistry(overrides ...MergeModuleSpec) (*MergeOpRegistry, error) {
	specs, err := BuildMergeModuleSpecs(overrides...)
	if err != nil {
		return nil, err
	}
	return NewMergeOpRegistry(specs...)
}

var defModuleTpls = []MergeModuleTemplate{
	{
		Name:        MergeModuleNameProfile,
		Description: "玩家档案、角色名、账号清理和玩家镜像合服模板",
		Features:    []string{MergeFeatureProfile, MergeFeatureRoleName, MergeFeaturePlayerMirror, MergeFeatureAccountClean},
		Required:    true,
		Checks:      []string{"角色名唯一键冲突", "账号绑定与玩家镜像完整性", "低活跃账号清理白名单", "Profile 抽样读写校验"},
		Evidence:    []string{"角色改名方案", "玩家镜像合并摘要", "账号清理名单", "Profile 抽样校验报告"},
		Rollback:    []string{"恢复角色名索引", "恢复玩家镜像键", "恢复被清理账号快照"},
	},
	{
		Name:        MergeModuleNameMail,
		Description: "个人邮件、全服邮件和合服补偿邮件模板",
		Features:    []string{MergeFeatureMail, MergeFeatureGlobalMail},
		Required:    true,
		Checks:      []string{"未领取邮件迁移", "全服邮件过滤策略", "补偿邮件去重", "邮件附件幂等发放"},
		Evidence:    []string{"邮件迁移批次", "全服邮件保留清单", "附件发放校验摘要"},
		Rollback:    []string{"删除新服邮件批次", "恢复旧服邮件索引"},
	},
	{
		Name:        MergeModuleNameLeaderboard,
		Description: "本地榜、跨服榜和排行榜补充信息合服模板",
		Features:    []string{MergeFeatureLeaderboard, MergeFeatureRank, MergeFeatureLocalRank, MergeFeatureCrossRank},
		Required:    true,
		Checks:      []string{"榜单排序稳定性", "同玩家多榜去重", "赛季结算窗口", "榜单补充信息一致性"},
		Evidence:    []string{"榜单重算摘要", "赛季窗口确认", "榜单抽样校验报告"},
		Rollback:    []string{"恢复旧榜快照", "撤销新榜重算批次"},
	},
	{
		Name:        MergeModuleNameGuild,
		Description: "公会、公会名、公会镜像和公会排行榜合服模板",
		Features:    []string{MergeFeatureGuild, MergeFeatureGuildName, MergeFeatureGuildRank, MergeFeatureGuildMirror},
		Required:    true,
		Checks:      []string{"公会名唯一键冲突", "公会成员与会长关系", "公会镜像完整性", "公会活动结算窗口"},
		Evidence:    []string{"公会改名方案", "公会镜像合并摘要", "公会榜重算报告"},
		Rollback:    []string{"恢复公会名索引", "恢复公会镜像", "撤销公会榜重算"},
	},
	{
		Name:        MergeModuleNameFriend,
		Description: "好友关系、黑名单和好友充值活动合服模板",
		Features:    []string{MergeFeatureFriend, MergeFeatureFriendRecharge},
		Required:    true,
		Checks:      []string{"好友双向边一致性", "黑名单优先级", "好友活动参与记录合并", "跨服好友引用"},
		Evidence:    []string{"好友边合并摘要", "好友活动合并批次"},
		Rollback:    []string{"恢复好友图谱快照", "撤销好友活动批次"},
	},
	{
		Name:        MergeModuleNameAuction,
		Description: "拍卖、战区拍卖和流拍记录合服模板",
		Features:    []string{MergeFeatureAuction, MergeFeatureWarzoneAuction},
		Required:    true,
		Checks:      []string{"拍卖结算窗口", "竞价订单幂等", "流拍记录去重", "战区归属确认"},
		Evidence:    []string{"拍卖冻结确认", "流拍记录合并摘要", "订单幂等校验报告"},
		Rollback:    []string{"恢复拍卖快照", "撤销合服后竞价写入"},
	},
	{
		Name:        MergeModuleNameActivity,
		Description: "活动状态、活动榜和运营活动过期数据合服模板",
		Features:    []string{MergeFeatureActivity, MergeFeatureActivityState, MergeFeatureActivityRank},
		Required:    true,
		Checks:      []string{"活动开放窗口", "活动状态去重", "活动榜结算", "奖励领取幂等"},
		Evidence:    []string{"活动状态合并摘要", "活动榜重算报告", "奖励领取抽样"},
		Rollback:    []string{"恢复活动状态快照", "撤销活动榜重算", "冻结异常奖励领取"},
	},
	{
		Name:        MergeModuleNamePayment,
		Description: "支付订单、IAP 订单和补单复核合服模板",
		Features:    []string{MergeFeaturePayment, MergeFeatureOrder, MergeFeatureIAP},
		Required:    true,
		Checks:      []string{"订单唯一键冲突", "已发货订单幂等", "未完成订单冻结", "补单复核窗口"},
		Evidence:    []string{"订单迁移批次", "发货幂等校验", "补单复核清单"},
		Rollback:    []string{"恢复订单索引", "冻结合服窗口补单"},
	},
	{
		Name:        MergeModuleNameServerInfo,
		Description: "区服合服信息、来源区服记录和路由元数据模板",
		Features:    []string{MergeFeatureServerGroup, MergeFeatureServerInfo},
		Required:    true,
		Checks:      []string{"来源区服记录", "目标区服版本", "路由元数据一致性", "合服批次号幂等"},
		Evidence:    []string{"合服批次记录", "来源区服映射", "路由元数据校验"},
		Rollback:    []string{"恢复合服信息快照", "恢复旧路由元数据"},
	},
}

func tmplPlaceholderSpec(template MergeModuleTemplate) (MergeModuleSpec, error) {
	normalized, err := normModuleTpl(template)
	if err != nil {
		return MergeModuleSpec{}, err
	}
	return normMergeSpec(MergeModuleSpec{
		Name:        normalized.Name,
		Description: normalized.Description,
		Features:    normalized.Features,
		Required:    normalized.Required,
		GenDB:       tmplPlaceholderFunc(normalized, MergeModulePhaseGenDB),
		Merge:       tmplPlaceholderFunc(normalized, MergeModulePhaseMerge),
		Rollback:    tmplPlaceholderFunc(normalized, MergeModulePhaseRollback),
	})
}

func templateOverrideSpec(override MergeModuleSpec) (MergeModuleSpec, error) {
	name := normalizeID(override.Name)
	if template, ok := FindMergeTemplate(name); ok {
		if override.Description == "" {
			override.Description = template.Description
		}
		if len(override.Features) == 0 {
			override.Features = template.Features
		}
		override.Required = override.Required || template.Required
	}
	return normMergeSpec(override)
}

func tmplPlaceholderFunc(template MergeModuleTemplate, phase string) MergeModuleFunc {
	template = cloneMergeTemplate(template)
	phase = normalizeID(phase)
	return func(ctx context.Context, input MergeModuleInput) (MergeModuleResult, error) {
		if err := ctxErr(ctx); err != nil {
			return MergeModuleResult{}, err
		}
		if !templateRequested(template, mergeInputReq(input)) {
			return MergeModuleResult{Skipped: true}, nil
		}
		return MergeModuleResult{
			Findings: []ConflictFinding{{
				Code:     "merge_module_template_unimplemented",
				Severity: MigrationSeverityBlocker,
				Subject:  template.Name,
				Detail:   "默认合服模块 " + template.Name + " 尚未接入 " + phase + " 阶段项目处理器",
			}},
			Meta: map[string]string{
				"template": "default",
				"phase":    phase,
			},
		}, nil
	}
}

func templateRequested(template MergeModuleTemplate, request MergeRequest) bool {
	features := requestedFeatures(request)
	if len(features) == 0 {
		return false
	}
	for _, feature := range template.Features {
		if containsID(features, feature) {
			return true
		}
	}
	return false
}

func normModuleTpl(template MergeModuleTemplate) (MergeModuleTemplate, error) {
	template.Name = normalizeID(template.Name)
	template.Description = firstNonEmpty(template.Description)
	template.Features = normalizeIDs(template.Features)
	template.Checks = cloneMergeStrings(template.Checks)
	template.Evidence = cloneMergeStrings(template.Evidence)
	template.Rollback = cloneMergeStrings(template.Rollback)
	if template.Name == "" {
		return MergeModuleTemplate{}, ErrMergeModuleInvalid
	}
	if len(template.Features) == 0 {
		return MergeModuleTemplate{}, ErrMergeModuleInvalid
	}
	return template, nil
}

func cloneMergeTemplates(templates []MergeModuleTemplate) []MergeModuleTemplate {
	if len(templates) == 0 {
		return nil
	}
	out := make([]MergeModuleTemplate, len(templates))
	for i, template := range templates {
		out[i] = cloneMergeTemplate(template)
	}
	return out
}

func cloneMergeTemplate(template MergeModuleTemplate) MergeModuleTemplate {
	template.Features = append([]string(nil), template.Features...)
	template.Checks = cloneMergeStrings(template.Checks)
	template.Evidence = cloneMergeStrings(template.Evidence)
	template.Rollback = cloneMergeStrings(template.Rollback)
	return template
}
