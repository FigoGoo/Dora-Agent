package a2ui

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestActionCardUsesBaseCardProtocolType(t *testing.T) {
	field, ok := reflect.TypeOf(Action{}).FieldByName("Card")
	if !ok {
		t.Fatalf("Action should expose Card field")
	}
	if field.Type != reflect.TypeOf(&Card{}) {
		t.Fatalf("Action.Card type = %s, want *a2ui.Card", field.Type)
	}
}

func TestInfoCollectionCardEmbedsBaseCard(t *testing.T) {
	card := InfoCollectionCard{
		Card: NewCard(CardTypeInfoCollection, "补充产品信息", "root", []Component{
			CardContainer("root", []string{"product", "asset"}),
			TextInput("product", TextInputComp{Key: "product_name", Label: "产品名称/品类", Required: true}),
			FileUpload("asset", FileUploadComp{Key: "reference_file", Label: "上传参考图", Accept: "image/*"}),
		}),
	}
	card.SubmitLabel = "提交"

	raw, err := json.Marshal(card)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	body := string(raw)
	for _, want := range []string{
		`"card_type":"info_collection"`,
		`"title":"补充产品信息"`,
		`"root":"root"`,
		`"submit_label":"提交"`,
		`"Card":{"children":["product","asset"]}`,
		`"TextInput":{"key":"product_name","label":"产品名称/品类","required":true}`,
		`"FileUpload":{"key":"reference_file","label":"上传参考图","accept":"image/*"}`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("card JSON missing %s in %s", want, body)
		}
	}
}
