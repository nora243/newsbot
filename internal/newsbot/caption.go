package newsbot

import (
	"strings"
	"unicode"
)

// Posting text stays separate from the on-screen headline and spoken script.
func postingCaption(s Script) string {
	if strings.TrimSpace(s.Title) == "" {
		return ""
	}
	text := " " + strings.Join(strings.Fields(strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return unicode.ToLower(r)
		}
		return ' '
	}, s.Title+" "+s.Summary)), " ") + " "
	tags := []string{"#SoHip", "#TinTuc"}
	rules := []struct {
		tag   string
		terms []string
	}{
		{"#Flydubai", []string{"flydubai"}}, {"#DonaldTrump", []string{"trump"}}, {"#AI", []string{"trí tuệ nhân tạo", "chatgpt", "gemini"}},
		{"#BongDa", []string{"bóng đá", "world cup", "premier league"}}, {"#CongNghe", []string{"công nghệ", "iphone", "samsung", "phần mềm", "trí tuệ nhân tạo"}},
		{"#KinhTe", []string{"kinh tế", "lãi suất", "giá vàng", "chứng khoán", "ngân hàng", "thuế", "giá dầu"}},
		{"#GiaoDuc", []string{"giáo dục", "học sinh", "sinh viên", "thi đại học"}}, {"#SucKhoe", []string{"sức khỏe", "bệnh viện", "bác sĩ", "dịch bệnh"}},
		{"#TheGioi", []string{"israel", "iran", "ukraine", "nga", "trump", "pháp", "nobel", "flydubai"}},
		{"#VietNam", []string{"việt nam", "hà nội", "tp hcm", "đà nẵng"}},
	}
	for _, rule := range rules {
		for _, term := range rule.terms {
			if strings.Contains(text, " "+term+" ") {
				tags = append(tags, rule.tag)
				break
			}
		}
		if len(tags) == 5 {
			break
		}
	}
	if len(tags) < 3 {
		tags = append(tags, "#TinMoi")
	}
	return strings.TrimSpace(s.Title) + "\n\n" + strings.Join(tags, " ")
}
