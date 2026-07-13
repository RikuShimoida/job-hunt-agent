package model

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"slices"
	"strings"
)

// 重要変更（material change）の対象項目。
//
// 通知本文全体をハッシュ化しないのは、加点理由の文言が変わっただけで
// 再通知され、通知がノイズになるため。判断に影響する項目だけを見る。
var materialFields = []struct {
	label string
	value func(JobPosting) string
}{
	{"単価", materialRate},
	{"リモート", materialRemote},
	{"開始時期", materialStart},
	{"必須スキル", materialSkills},
}

// MaterialFields は重要変更の対象項目を安定した文字列表現へ落とす。
func MaterialFields(j JobPosting) []string {
	out := make([]string, 0, len(materialFields))
	for _, f := range materialFields {
		out = append(out, f.label+"="+f.value(j))
	}
	return out
}

// MaterialHash は重要変更の対象項目から導出したハッシュ。
// notifications.payload_hash に保存し、再通知の要否判定に使う。
func MaterialHash(j JobPosting) string {
	sum := sha256.Sum256([]byte(strings.Join(MaterialFields(j), "\n")))
	return hex.EncodeToString(sum[:])
}

// MaterialChanges は before から after への重要変更を人が読める形で返す。
// 変更がなければ空を返す。
func MaterialChanges(before, after JobPosting) []string {
	var changes []string
	for _, f := range materialFields {
		b, a := f.value(before), f.value(after)
		if b == a {
			continue
		}
		changes = append(changes, fmt.Sprintf("%s: %s → %s", f.label, b, a))
	}
	return changes
}

func materialRate(j JobPosting) string {
	if j.RateMin == nil && j.RateMax == nil {
		return "不明"
	}
	return fmt.Sprintf("%s %s〜%s", j.RateType, formatOptionalInt(j.RateMin), formatOptionalInt(j.RateMax))
}

func materialRemote(j JobPosting) string {
	if j.OnsiteDays == nil {
		return string(j.RemoteType)
	}
	return fmt.Sprintf("%s 週%d日出社", j.RemoteType, *j.OnsiteDays)
}

func materialStart(j JobPosting) string {
	if j.StartDate == nil {
		return "不明"
	}
	return j.StartDate.UTC().Format("2006-01-02")
}

// materialSkills は並び順の違いを変更と誤検知しないよう、複製をソートしてから連結する。
func materialSkills(j JobPosting) string {
	if len(j.RequiredSkills) == 0 {
		return "なし"
	}
	skills := slices.Clone(j.RequiredSkills)
	slices.Sort(skills)
	return strings.Join(skills, "、")
}

func formatOptionalInt(v *int) string {
	if v == nil {
		return "不明"
	}
	return fmt.Sprintf("%d", *v)
}

// SourceFailure はソース1つぶんの取得失敗。エラー通知の入力になる。
type SourceFailure struct {
	SourceName string
	Message    string
}
