package hub

import "testing"

// TestIsBareNumber 覆盖裸数字判定：与前端 isHubNumber / Console.Handle 的
// strconv.Atoi+n>0 判定同口径——全角数字归一化后为正整数才成立。
func TestIsBareNumber(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"2", true},
		{"03", true},
		{"１２", true}, // 全角数字归一化
		{"０", false},  // 全角 0：归一化后为 0，非有效编号
		{"0", false},
		{"000", false},
		{"abc", false},
		{"", false},
		{"-1", false},
		{"2a", false},
		{" 2", false}, // 含空白：调用方负责 TrimSpace，helper 不代做
		{"1 2", false},
	}
	for _, tc := range cases {
		if got := IsBareNumber(tc.in); got != tc.want {
			t.Errorf("IsBareNumber(%q)=%v, want %v", tc.in, got, tc.want)
		}
	}
}
