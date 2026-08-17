package protocol

import "time"

// AuthResponse 是认证成功后返回给客户端的登录令牌响应。
type AuthResponse struct {
	Result    string      `json:"result"`
	Authtoken string      `json:"authtoken"`
	ShardRole []ShardRole `json:"shardrole"`
}

// ShardRole 描述账号在单个分区里的已有角色。
type ShardRole struct {
	Shard string `json:"shard"`
	Name  string `json:"name"`
	LLT   int64  `json:"llt"`
}

// ShardListResponse 是分区列表接口响应。
type ShardListResponse struct {
	Shards []ShardDescriptor `json:"shards"`
}

// ShardDescriptor 描述客户端选区页展示的单个分区。
type ShardDescriptor struct {
	Name          string `json:"name"`
	DisplayName   string `json:"dn"`
	ShowState     int32  `json:"ss"`
	Recommend     int32  `json:"recmd"`
	RecommendLang string `json:"recmd_lang"`
	VGID          int32  `json:"v_gid"`
	MainlineLevel string `json:"mainline_level"`
}

// GetGateResponse 是网关寻址接口响应。
type GetGateResponse struct {
	Result     string `json:"result"`
	IP         string `json:"ip"`
	LoginToken string `json:"logintoken"`
}

// NoticeResponse 是登录前公告和端点发现响应。
type NoticeResponse struct {
	Endpoint             map[string]string `json:"Endpoint"`
	ServerTime           string            `json:"ServerTime"`
	TimeoutTime          string            `json:"TimeoutTime"`
	TokenTime            string            `json:"TokenTime"`
	Publics              []any             `json:"Publics"`
	Chatserver           string            `json:"Chatserver"`
	ChatIp               string            `json:"chatIp"`
	NoticeCustomer       string            `json:"NoticeCustomer"`
	WinScanSwitch        int               `json:"WinScanSwitch"`
	AndroidPrivacySwitch string            `json:"AndroidPrivacySwitch"`
	IosPrivacySwitch     string            `json:"IosPrivacySwitch"`
	ClientExpireTime     string            `json:"clientExpireTime"`
}

// BuildAuthOK 构造认证成功响应。
func BuildAuthOK(authToken string) AuthResponse {
	return AuthResponse{
		Result:    "ok",
		Authtoken: authToken,
		ShardRole: []ShardRole{},
	}
}

// BuildShardList 构造分区列表响应并隔离调用方切片。
func BuildShardList(shards []ShardDescriptor) ShardListResponse {
	return ShardListResponse{Shards: append([]ShardDescriptor(nil), shards...)}
}

// BuildGetGate 构造网关寻址响应。
func BuildGetGate(ip, loginToken string) GetGateResponse {
	return GetGateResponse{
		Result:     "ok",
		IP:         ip,
		LoginToken: loginToken,
	}
}

// BuildNotice 构造公告响应并隔离端点表。
func BuildNotice(endpoints map[string]string, chatListen string, now time.Time) NoticeResponse {
	var copiedEndpoints map[string]string
	if endpoints != nil {
		copiedEndpoints = make(map[string]string, len(endpoints))
		for key, value := range endpoints {
			copiedEndpoints[key] = value
		}
	}
	return NoticeResponse{
		Endpoint:             copiedEndpoints,
		ServerTime:           formatUnix(now),
		TimeoutTime:          "30",
		TokenTime:            "12900",
		Publics:              []any{},
		Chatserver:           chatListen,
		ChatIp:               chatListen,
		NoticeCustomer:       "",
		WinScanSwitch:        0,
		AndroidPrivacySwitch: "false",
		IosPrivacySwitch:     "false",
		ClientExpireTime:     "0",
	}
}
