package a2ui

import "testing"

func TestParseActionEnvelopeContentAcceptsOnlyPureEnvelope(t *testing.T) {
	content := `{"a2ui_version":"1.0","actions":[{"type":"append_card","surface":"chat","card_id":"brief-intake","card":{"card_type":"info_collection","root":"root","components":[{"id":"root","component":{"Card":{"children":["product"]}}},{"id":"product","component":{"TextInput":{"key":"product_name","label":"产品名称/品类"}}}]}}]}`

	envelope, ok := ParseActionEnvelopeContent(content)

	if !ok {
		t.Fatalf("ParseActionEnvelopeContent() should accept pure ActionEnvelope JSON")
	}
	if envelope.Version != Version1 || len(envelope.Actions) != 1 || envelope.Actions[0].CardID != "brief-intake" {
		t.Fatalf("envelope = %#v", envelope)
	}
	if envelope.Actions[0].Card == nil || envelope.Actions[0].Card.Root != "root" || envelope.Actions[0].Card.Type != CardTypeInfoCollection {
		t.Fatalf("card = %#v", envelope.Actions[0].Card)
	}
}

func TestParseActionEnvelopeContentRejectsMarkdownWrappedEnvelope(t *testing.T) {
	content := "好的，先补充信息：\n" +
		`{"a2ui_version":"1.0","actions":[{"type":"append_card","card_id":"brief-intake"}]}`

	if _, ok := ParseActionEnvelopeContent(content); ok {
		t.Fatalf("ParseActionEnvelopeContent() should reject text wrapped around ActionEnvelope")
	}
}

func TestParseActionEnvelopeContentRejectsCodeFence(t *testing.T) {
	content := "```json\n" +
		`{"a2ui_version":"1.0","actions":[{"type":"append_card","card_id":"brief-intake"}]}` +
		"\n```"

	if _, ok := ParseActionEnvelopeContent(content); ok {
		t.Fatalf("ParseActionEnvelopeContent() should reject Markdown code fences")
	}
}

func TestParseActionEnvelopeContentRejectsAppendCardWithoutCardRoot(t *testing.T) {
	content := `{"a2ui_version":"1.0","actions":[{"type":"append_card","surface":"chat","card_id":"brief-intake","card":{"root":"product","components":[{"id":"product","component":{"TextInput":{"key":"product_name"}}}]}}]}`

	if _, ok := ParseActionEnvelopeContent(content); ok {
		t.Fatalf("ParseActionEnvelopeContent() should reject append_card without a Card root component")
	}
}

func TestParseActionEnvelopeContentAcceptsInteractiveCardWithoutSubmitTemplate(t *testing.T) {
	content := `{"a2ui_version":"1.0","actions":[{"type":"append_card","surface":"chat","card_id":"brief-intake","card":{"root":"root","components":[{"id":"root","component":{"Card":{"children":["product","asset"]}}},{"id":"product","component":{"TextInput":{"key":"product_name"}}},{"id":"asset","component":{"FileUpload":{"key":"reference_file","label":"上传图片"}}}]}}]}`

	if _, ok := ParseActionEnvelopeContent(content); !ok {
		t.Fatalf("ParseActionEnvelopeContent() should accept interactive cards without submit template")
	}
}

func TestEnsureActionInstanceIDsAssignsDistinctCardIDsPerAppendCard(t *testing.T) {
	envelope := ActionEnvelope{
		Version: Version1,
		Actions: []Action{
			{
				Type:    ActionAppendCard,
				Surface: "chat",
				CardID:  "skill-selection",
				Card: &Card{
					Root: "root",
					Components: []Component{
						CardContainer("root", []string{"choice"}),
						NewComponent("choice", ComponentSingleChoice, ChoiceComp{Key: "skill"}),
					},
				},
			},
			{
				Type:    ActionAppendCard,
				Surface: "chat",
				CardID:  "skill-selection",
				Card: &Card{
					Root: "root",
					Components: []Component{
						CardContainer("root", []string{"choice"}),
						NewComponent("choice", ComponentSingleChoice, ChoiceComp{Key: "skill"}),
					},
				},
			},
		},
	}
	nextID := sequentialTestIDs("card-instance-1", "card-instance-2")

	got := EnsureActionInstanceIDs(envelope, nextID)

	if got.Actions[0].CardID != "skill-selection:card-instance-1" || got.Actions[1].CardID != "skill-selection:card-instance-2" {
		t.Fatalf("card ids = %#v", got.Actions)
	}
}

func sequentialTestIDs(ids ...string) func() string {
	index := 0
	return func() string {
		if index >= len(ids) {
			return ids[len(ids)-1]
		}
		id := ids[index]
		index++
		return id
	}
}
