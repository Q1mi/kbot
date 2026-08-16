package skill

import "testing"

const validSkill = "---\nname: triage\ndescription: route cases\nallowed-tools: []\n---\n# Steps\nCheck evidence."

func TestSkillDraftMustBePublishedBeforeRuntimeResolve(t *testing.T) {
	service := NewService()
	item, _, err := service.Create(t.Context(), "ws", "claims", []byte(validSkill))
	if err != nil {
		t.Fatal(err)
	}
	draft, err := service.CreateVersion(t.Context(), "ws", item.ID, []byte(validSkill))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Resolve(t.Context(), "ws", draft.ID); err == nil {
		t.Fatal("draft resolved before publish")
	}
	if err := service.PublishVersion("ws", item.ID, draft.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Resolve(t.Context(), "ws", draft.ID); err != nil {
		t.Fatal(err)
	}
}
