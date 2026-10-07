package skills

import "testing"

func TestParseSkillMD(t *testing.T) {
	name, desc, err := ParseSkillMD([]byte("---\nname: brainstorming\ndescription: explore ideas\n---\n# Hi\n"))
	if err != nil || name != "brainstorming" || desc != "explore ideas" {
		t.Fatalf("%q %q %v", name, desc, err)
	}
	if _, _, err := ParseSkillMD([]byte("no frontmatter")); err != errInvalidSkill {
		t.Fatalf("got %v", err)
	}
	if _, _, err := ParseSkillMD([]byte("---\nname: x\n---\n")); err != errInvalidSkill {
		t.Fatalf("missing desc: %v", err)
	}
}

func TestBodyPreview(t *testing.T) {
	got := BodyPreview([]byte("---\nname: a\ndescription: b\n---\nhello world"), 5)
	if got != "hello" {
		t.Fatalf("%q", got)
	}
}
