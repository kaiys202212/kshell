package workbuddy

import "testing"

func TestAppExecutableCandidatesOrder(t *testing.T) {
	got := appExecutableCandidates(
		`C:\Users\u\AppData\Local`,
		`C:\Program Files`,
		`C:\Program Files (x86)`,
	)
	want := []string{
		`C:\Users\u\AppData\Local\Programs\WorkBuddy\WorkBuddy.exe`,
		`C:\Users\u\AppData\Local\Programs\WorkBuddy\WorkBuddyAI.exe`,
		`C:\Users\u\AppData\Local\WorkBuddy\WorkBuddy.exe`,
		`C:\Users\u\AppData\Local\WorkBuddy\WorkBuddyAI.exe`,
		`C:\Users\u\AppData\Local\Programs\WorkBuddyAI\WorkBuddy.exe`,
		`C:\Users\u\AppData\Local\Programs\WorkBuddyAI\WorkBuddyAI.exe`,
		`C:\Users\u\AppData\Local\WorkBuddyAI\WorkBuddy.exe`,
		`C:\Users\u\AppData\Local\WorkBuddyAI\WorkBuddyAI.exe`,
		`C:\Program Files\Programs\WorkBuddy\WorkBuddy.exe`,
		`C:\Program Files\Programs\WorkBuddy\WorkBuddyAI.exe`,
		`C:\Program Files\WorkBuddy\WorkBuddy.exe`,
		`C:\Program Files\WorkBuddy\WorkBuddyAI.exe`,
		`C:\Program Files\Programs\WorkBuddyAI\WorkBuddy.exe`,
		`C:\Program Files\Programs\WorkBuddyAI\WorkBuddyAI.exe`,
		`C:\Program Files\WorkBuddyAI\WorkBuddy.exe`,
		`C:\Program Files\WorkBuddyAI\WorkBuddyAI.exe`,
		`C:\Program Files (x86)\Programs\WorkBuddy\WorkBuddy.exe`,
		`C:\Program Files (x86)\Programs\WorkBuddy\WorkBuddyAI.exe`,
		`C:\Program Files (x86)\WorkBuddy\WorkBuddy.exe`,
		`C:\Program Files (x86)\WorkBuddy\WorkBuddyAI.exe`,
		`C:\Program Files (x86)\Programs\WorkBuddyAI\WorkBuddy.exe`,
		`C:\Program Files (x86)\Programs\WorkBuddyAI\WorkBuddyAI.exe`,
		`C:\Program Files (x86)\WorkBuddyAI\WorkBuddy.exe`,
		`C:\Program Files (x86)\WorkBuddyAI\WorkBuddyAI.exe`,
	}
	if len(got) != len(want) {
		t.Fatalf("候选数量不符: got %d want %d\n%v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("候选[%d]不符:\n got %q\nwant %q", i, got[i], want[i])
		}
	}
}

func TestAppExecutableCandidatesEmptyBaseSkipped(t *testing.T) {
	got := appExecutableCandidates("", `C:\Program Files`, "")
	if len(got) != 8 {
		t.Fatalf("仅 ProgramFiles 应产生 8 个候选: %v", got)
	}
	for _, c := range got {
		if len(c) == 0 || c[:2] != `C:` {
			t.Fatalf("空基础目录产生了非预期候选: %q", c)
		}
	}
}
