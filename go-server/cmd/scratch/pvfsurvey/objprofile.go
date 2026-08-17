package main

import (
	"fmt"
	"sort"
	"strings"

	dnfpvf "longheng.io/server/internal/modules/dnf/pvf"
	"longheng.io/server/internal/modules/dnf/worldmap"
	platformpvf "longheng.io/server/internal/platform/pvf"
)

// objProfile 是一个被动对象定义文件（.obj）的行为画像。
type objProfile struct {
	ID                int64    `json:"id"`
	Ref               string   `json:"ref"`
	Found             bool     `json:"found"`
	Sections          []string `json:"sections,omitempty"`
	PassiveType       string   `json:"passive_type,omitempty"`
	PassiveSubType    string   `json:"passive_sub_type,omitempty"`
	DestroyConditions []string `json:"destroy_conditions,omitempty"`
	ActRefs           []string `json:"act_refs,omitempty"`
	BehaviorClass     string   `json:"behavior_class"`
}

// 呈现性 section（不构成行为语义）
var presentationSections = map[string]bool{
	"layer": true, "name": true, "width": true, "floating height": true,
	"basic motion": true, "etc motion": true, "int data": true, "string data": true,
	"layer level": true, "sound category": true, "create sound": true,
	"destroy particle": true, "add particles": true, "particle": true, "file": true,
	"ani path": true, "ani title": true, "screenshot data": true, "movie unit": true,
	"play bgm": true, "face image": true, "draw name": true, "speech": true, "talk": true,
	"talk group data": true, "moving particle": true, "add object effect": true,
	"transparent on meet player": true, "absolute zpos": true, "init rotation": true,
	"sync animation rotation": true, "diff rotation": true, "max rotation": true,
	"isometric cell map info": true, "blocking area": true, "motion type": true,
	"add tail image": true, "draw tail": true, "draw tail use": true,
	"tail bottom up layer": true, "tail rotate": true, "tail type": true,
	"basic animation": true, "notice": true, "category": true, "type": true,
}

// buildObjProfiles 为给定对象集合读取 .obj 定义并归类行为：
//
//	scripted_action    含 [basic action]/[etc action]（行为脚本在 .act 内）
//	declared_behavior  无动作脚本但有行为字段（type/sub type/destroy condition/hp/team/attack info/trap 等）
//	presentation_only  仅呈现性 section（如电梯三件套）
//	missing            lst 有条目但归档内无文件
func buildObjProfiles(archive *platformpvf.Archive, objRefs map[int64]string, ids map[int64]bool) map[int64]*objProfile {
	profiles := map[int64]*objProfile{}
	for id := range ids {
		ref, ok := objRefs[id]
		if !ok {
			profiles[id] = &objProfile{ID: id, BehaviorClass: "not_in_lst"}
			continue
		}
		profile := &objProfile{ID: id, Ref: ref}
		profiles[id] = profile
		text, found := readObjText(archive, ref)
		if !found {
			profile.BehaviorClass = "missing"
			continue
		}
		profile.Found = true
		doc, err := worldmap.ParseDocument(ref, text)
		if err != nil {
			profile.BehaviorClass = "parse_error"
			continue
		}
		seen := map[string]bool{}
		hasAction := false
		hasBehavior := false
		for _, section := range doc.Sections {
			name := strings.ToLower(strings.TrimSpace(section.Name))
			if name == "" || strings.HasPrefix(name, "/") || seen[name] {
				continue
			}
			seen[name] = true
			profile.Sections = append(profile.Sections, name)
			switch name {
			case "basic action", "etc action", "last action":
				hasAction = true
				if name == "basic action" {
					profile.ActRefs = append(profile.ActRefs, sectionTexts(doc, section)...)
				}
			case "passive object type":
				hasBehavior = true
				profile.PassiveType = strings.Join(sectionTexts(doc, section), " ")
			case "passive object sub type":
				hasBehavior = true
				profile.PassiveSubType = strings.Join(sectionTexts(doc, section), " ")
			case "object destroy condition":
				hasBehavior = true
				profile.DestroyConditions = sectionTexts(doc, section)
			default:
				if !presentationSections[name] {
					hasBehavior = true
				}
			}
		}
		sort.Strings(profile.Sections)
		switch {
		case hasAction:
			profile.BehaviorClass = "scripted_action"
		case hasBehavior:
			profile.BehaviorClass = "declared_behavior"
		default:
			profile.BehaviorClass = "presentation_only"
		}
	}
	return profiles
}

func sectionTexts(doc *dnfpvf.Document, section dnfpvf.Section) []string {
	if section.Start < 0 || section.End > len(doc.Tokens) || section.Start > section.End {
		return nil
	}
	var out []string
	for _, token := range doc.Tokens[section.Start:section.End] {
		switch token.Kind {
		case dnfpvf.TokenString, dnfpvf.TokenIdent:
			value := token.Value
			if value == "" {
				value = token.Raw
			}
			if value != "" {
				out = append(out, value)
			}
		case dnfpvf.TokenInt:
			out = append(out, fmt.Sprintf("%d", token.Int))
		}
	}
	return out
}
