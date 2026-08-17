package dnfbridge

import (
	"context"
	dnfcharstat "longheng.io/server/internal/modules/dnf/charstat"
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
	"strconv"
)

const csharpLegacyUserInfoKind = "legacy_userinfo"

func csharpLegacyUserInfoInitPackets() []csharpSelectInitPacket {
	// 0x015d 是角色栏解锁相关通知，不能放进选择角色主动初始化；基础 8 栏由 roster 字段默认开放。
	// C# registry 中部分上下文刷新包仅保留 builder，不放入 active init，避免进入角色后重复推栏位/上下文状态。
	ids := []uint16{
		0x0023, 0x0047,
		0x0057, 0x0058, 0x0059, 0x005b, 0x005c, 0x005f, 0x005a,
		0x0060, 0x0064, 0x0067, 0x006a, 0x006b, 0x0073, 0x007a,
		0x0080, 0x0081, 0x0082, 0x0083, 0x0085, 0x0086, 0x0087,
		0x0088, 0x0089, 0x008f, 0x0090, 0x0091, 0x0092, 0x0098,
		0x009b, 0x00a0, 0x00a1, 0x00a2, 0x00a3, 0x00aa, 0x00b0,
		0x00b6, 0x00bc, 0x00c8, 0x00c9, 0x00cf, 0x00d0, 0x00d1,
		0x00d2, 0x00d3, 0x00d5, 0x00d6, 0x00d7, 0x00d8, 0x00dc,
		0x00dd, 0x00df, 0x00e0, 0x00e3, 0x00e6, 0x00eb, 0x00fe,
		0x00ff, 0x0103, 0x0109, 0x010c, 0x0114, 0x0115, 0x0117,
		0x0118, 0x011d, 0x0126, 0x012a, 0x012d, 0x0154, 0x0159,
		0x017c, 0x0182, 0x0183, 0x0184, 0x0186, 0x0192,
		0x022d, 0x022e, 0x0237, 0x0238, 0x0253, 0x0254, 0x0255,
		0x025b, 0x026e, 0x0274, 0x0275, 0x0276, 0x0287, 0x028a,
		0x028b, 0x029f, 0x02a9, 0x02aa, 0x02b0, 0x02bc, 0x02c1,
		0x02d2, 0x02d3, 0x02d8, 0x02ef, 0x0311, 0x0312, 0x031d,
		0x0324, 0x0336, 0x034b,
		0x034c, 0x034d, 0x034e, 0x0352, 0x0354, 0x0355, 0x0359,
		0x036b, 0x037b, 0x0393, 0x03c7, 0x03cd, 0x03d0,
		0x03e6, 0x03e7, 0x03e8, 0x03f3, 0x03fd, 0x0400, 0x0406,
		0x0407, 0x0408, 0x0409, 0x040a, 0x040b, 0x040c, 0x040d,
		0x040e, 0x040f, 0x0410, 0x0411, 0x0412, 0x0413, 0x0415,
		0x0416, 0x0418, 0x0419, 0x041a, 0x041b, 0x041c, 0x041d,
		0x041e, 0x0424, 0x0425, 0x0428, 0x0429, 0x042a, 0x042c,
		0x042d, 0x042e, 0x0430, 0x0435, 0x043e, 0x043f, 0x0440,
		0x044e, 0x044f, 0x0457, 0x0458, 0x0459, 0x045a, 0x045f,
		0x0462, 0x0469, 0x046b, 0x046c, 0x046e, 0x046f, 0x0470,
		0x048c, 0x048e, 0x048f, 0x04a6, 0x04a7, 0x04a9, 0x04aa,
		0x04b0, 0x04b1, 0x04b2, 0x04c6, 0x04c7, 0x04c8, 0x04ca,
		0x04d4, 0x04d5, 0x04da, 0x04db, 0x04df, 0x04e2, 0x04ec,
		0x04f0, 0x04f1, 0x04f8, 0x04fe, 0x04ff, 0x0508, 0x050a,
		0x050b, 0x050c, 0x050d, 0x0514, 0x0515, 0x0516, 0x0517,
		0x0518, 0x052b, 0x052c, 0x052f, 0x0532, 0x0533, 0x0534,
		0x0547, 0x0549,
	}
	packets := make([]csharpSelectInitPacket, 0, len(ids))
	for _, id := range ids {
		packets = append(packets, csharpSelectInitPacket{class: 0, msgID: id, kind: csharpLegacyUserInfoKind})
	}
	return packets
}

type csharpLegacyUserInfoReader struct {
	ctx         context.Context
	repo        dnfrepo.LegacyUserInfoRepository
	characterID string
	service     *Service
	session     *gameSession
	pvfStats    dnfcharstat.Vector
	hasPVFStats bool
	loaded      bool
}

func (s *Service) buildCSharpLegacyUserInfoBody(ctx context.Context, session *gameSession, repo dnfrepo.LegacyUserInfoRepository, charID uint16, msgID uint16) ([]byte, bool) {
	if repo == nil {
		return nil, false
	}
	reader := csharpLegacyUserInfoReader{
		ctx:         ctx,
		repo:        repo,
		characterID: strconv.Itoa(int(charID)),
		service:     s,
		session:     session,
	}
	var body []byte
	switch msgID {
	case 0x0023:
		body = reader.build23()
	case 0x0047:
		body = reader.build47()
	case 0x0057:
		body = reader.build57()
	case 0x0058:
		body = reader.build58()
	case 0x0059:
		body = reader.build59()
	case 0x005b:
		body = reader.build5B()
	case 0x005c:
		body = reader.build5C()
	case 0x005f:
		body = reader.build5F()
	case 0x005a, 0x0082, 0x00e3, 0x0103, 0x0114, 0x0115, 0x012d, 0x0186, 0x0192, 0x01d5, 0x01d6, 0x0376:
		return nil, true
	case 0x0060:
		body = reader.build60()
	case 0x0064:
		body = reader.buildOneU16("legacy_character_userinfo64_state", "object_key")
	case 0x0067:
		body = reader.buildTwoU32("legacy_character_userinfo67_state", "value_a", "value_b")
	case 0x006a:
		body = reader.build6A()
	case 0x006b:
		body = reader.buildOneU16("legacy_character_userinfo6b_state", "object_key")
	case 0x0073:
		body = reader.build73()
	case 0x007a:
		body = reader.build7A()
	case 0x0080:
		body = reader.build80()
	case 0x0081:
		body = reader.buildOneU16("legacy_character_userinfo81_state", "value")
	case 0x0083:
		body = reader.buildOneU16("legacy_character_userinfo83_state", "value")
	case 0x0085:
		body = reader.buildOneU16("legacy_character_userinfo85_state", "object_key")
	case 0x0086:
		body = reader.build86()
	case 0x0087:
		body = reader.buildOneU16("legacy_character_userinfo87_state", "value")
	case 0x0088:
		body = reader.build88()
	case 0x0089:
		body = reader.buildOneU8("legacy_character_userinfo89_state", "state")
	case 0x008f:
		body = reader.build8F()
	case 0x0090:
		body = reader.build90()
	case 0x0091:
		body = reader.build91()
	case 0x0092:
		body = reader.buildOneU8("legacy_character_userinfo92_state", "mode_flag")
	case 0x0098:
		body = reader.build98()
	case 0x009b:
		body = reader.buildTwoU32("legacy_character_userinfo9b_state", "value_a", "value_b")
	case 0x00a0:
		body = reader.buildA0()
	case 0x00a1:
		body = reader.buildA1()
	case 0x00a2:
		body = reader.buildA2()
	case 0x00a3:
		body = reader.buildOneU32("legacy_character_userinfoa3_state", "value")
	case 0x00aa:
		body = reader.buildAA()
	case 0x00b0:
		body = reader.buildOneU8("legacy_character_userinfob0_state", "enabled")
	case 0x00b6:
		body = reader.buildB6()
	case 0x00bc:
		body = reader.buildOneU8("legacy_character_userinfobc_state", "state")
	case 0x00c8:
		body = reader.buildOneU8("legacy_character_userinfoc8_state", "delta")
	case 0x00c9:
		body = reader.buildC9()
	case 0x00cf:
		body = reader.buildOneU32("legacy_character_userinfocf_state", "value")
	case 0x00d0:
		body = reader.buildD0()
	case 0x00d1:
		body = reader.buildD1()
	case 0x00d2:
		body = reader.buildD2()
	case 0x00d3:
		body = reader.buildD3()
	case 0x00d5:
		body = reader.buildD5Like("legacy_character_userinfod5_state")
	case 0x00d6:
		body = reader.buildD6()
	case 0x00d7:
		body = reader.buildD7()
	case 0x00d8:
		body = reader.buildD8()
	case 0x00dc:
		body = reader.buildOneU32("legacy_character_userinfodc_state", "value")
	case 0x00dd:
		body = reader.buildTwoU32("legacy_character_userinfodd_state", "value_a", "value_b")
	case 0x00df:
		body = reader.buildDF()
	case 0x00e0:
		body = reader.buildE0()
	case 0x00e6:
		body = reader.buildOneU8("legacy_character_userinfoe6_state", "value")
	case 0x00eb:
		body = reader.buildOneU16("legacy_character_userinfoeb_state", "value")
	case 0x00fe:
		body = reader.buildFE()
	case 0x00ff:
		body = reader.buildD5Like("legacy_character_userinfoff_state")
	case 0x0109:
		body = reader.build109()
	case 0x010c:
		body = reader.buildOneU16("legacy_character_userinfo10c_state", "value")
	case 0x0117:
		body = reader.buildOneU32("legacy_character_userinfo117_state", "value")
	case 0x0118:
		body = reader.build118()
	case 0x011d:
		body = reader.build11D()
	case 0x0126:
		body = reader.build126()
	case 0x012a:
		body = reader.buildD5Like("legacy_character_userinfo12a_state")
	case 0x0154:
		body = reader.build154()
	case 0x0159:
		body = reader.build159()
	case 0x017c:
		body = reader.build17C()
	case 0x0182:
		body = reader.build182()
	case 0x0183:
		body = reader.build183()
	case 0x0184:
		body = reader.build184()
	case 0x01bf:
		body = reader.build1BF()
	case 0x022d:
		body = reader.build22D()
	case 0x022e:
		body = reader.build22E()
	case 0x0237:
		body = reader.buildConditionalOneU32("legacy_character_userinfo237_state", "value")
	case 0x0238:
		body = reader.buildConditionalOneU32("legacy_character_userinfo238_state", "value")
	case 0x0253:
		body = reader.build253()
	case 0x0254:
		body = reader.build254()
	case 0x0255:
		body = reader.buildConditionalOneU16("legacy_character_userinfo255_state", "value")
	case 0x025b:
		body = reader.build25B()
	case 0x026e:
		body = reader.build26E()
	case 0x0274:
		body = reader.buildConditionalOneU32("legacy_character_userinfo274_state", "value")
	case 0x0275:
		body = reader.build275()
	case 0x0276:
		body = reader.build276()
	case 0x0287:
		body = reader.build287()
	case 0x028a:
		body = reader.build28A()
	case 0x028b:
		body = reader.build28B()
	case 0x029f:
		body = reader.build29F()
	case 0x02a9:
		body = reader.build2A9()
	case 0x02aa:
		body = reader.build2AA()
	case 0x02b0:
		body = reader.build2B0()
	case 0x02bc:
		body = reader.build2BC()
	case 0x02c1:
		body = reader.build2C1()
	case 0x02d2:
		body = reader.build2D2()
	case 0x02d3:
		body = reader.buildOneU32IfPresent("legacy_character_userinfo2d3_state", "value")
	case 0x02d8:
		body = reader.buildOneU32IfPresent("legacy_character_userinfo2d8_state", "value")
	case 0x02ef:
		body = reader.buildOneU32IfPresent("legacy_character_userinfo2ef_state", "value")
	case 0x031d:
		body = reader.buildOneU8IfPresent("legacy_character_userinfo31d_state", "value")
	case 0x0324:
		body = reader.build324()
	case 0x0327:
		body = reader.build327()
	case 0x0329:
		body = reader.build329()
	case 0x0336:
		body = reader.build336()
	case 0x034b:
		body = reader.build34B()
	case 0x034c:
		body = reader.build34CText()
	case 0x034d:
		body = reader.build34DValue()
	case 0x034e:
		body = reader.build34EByte()
	case 0x0352:
		body = reader.build352()
	case 0x0354:
		body = reader.build354()
	case 0x0355:
		body = reader.buildOneU32IfPresent("legacy_character_userinfo355_state", "value")
	case 0x0359:
		body = reader.build359()
	case 0x036b:
		body = reader.build36B()
	case 0x0161:
		body = reader.buildRawFixed(0x0161, 1)
	case 0x01d4:
		body = reader.buildRawByteCountList(0x01d4, 8)
	case 0x01d7:
		body = reader.buildRawByteCountList(0x01d7, 7)
	case 0x01d8:
		body = reader.buildRawByteCountList(0x01d8, 7)
	case 0x01d9:
		body = reader.buildRawFixed(0x01d9, 1)
	case 0x0343:
		body = reader.buildRawFixed(0x0343, 1)
	case 0x0344:
		body = reader.buildRawFixed(0x0344, 24)
	case 0x0373:
		body = reader.buildRawFixed(0x0373, 8)
	case 0x0374:
		body = reader.build374()
	case 0x0375:
		body = reader.buildRawFixed(0x0375, 38)
	case 0x0377:
		body = reader.buildRawFixed(0x0377, 8)
	case 0x0378:
		body = reader.buildRawFixed(0x0378, 5)
	case 0x0379:
		body = reader.build379()
	case 0x037a:
		body = reader.build37A()
	case 0x037b:
		body = reader.build37B()
	case 0x0393:
		body = reader.build393()
	case 0x03cd:
		body = reader.build3CD()
	case 0x03e6:
		body = reader.buildOneU8IfPresent("legacy_character_userinfo3e6_state", "value")
	default:
		if expectedLength, ok := csharpRawFixedUserInfoLength(msgID); ok {
			body = reader.buildRawFixed(msgID, expectedLength)
		} else if csharpRawUserInfoBody(msgID) {
			body = reader.buildRawBody(msgID)
		} else {
			return nil, false
		}
	}
	if !reader.loaded {
		return nil, false
	}
	return body, true
}
