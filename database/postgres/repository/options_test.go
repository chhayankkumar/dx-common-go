package repository

import "testing"

type widget struct {
	ID   string
	Name string
}

// describedWidget implements dao.TableDescriber — New should infer its
// table/ID column with zero explicit options.
type describedWidget struct {
	WidgetID string
}

func (describedWidget) TableName() string { return "described_widgets" }
func (describedWidget) IDColumn() string  { return "widget_id" }

func TestNew_WithTableAndID(t *testing.T) {
	b := New[widget](nil, WithTable[widget]("widgets"), WithID[widget]("widget_id"))

	if b.dao.TableName != "widgets" {
		t.Fatalf("TableName = %q, want %q", b.dao.TableName, "widgets")
	}
	if b.dao.IDColumn != "widget_id" {
		t.Fatalf("IDColumn = %q, want %q", b.dao.IDColumn, "widget_id")
	}
}

func TestNew_FallsBackToTableDescriber(t *testing.T) {
	b := New[describedWidget](nil)

	if b.dao.TableName != "described_widgets" {
		t.Fatalf("TableName = %q, want %q", b.dao.TableName, "described_widgets")
	}
	if b.dao.IDColumn != "widget_id" {
		t.Fatalf("IDColumn = %q, want %q", b.dao.IDColumn, "widget_id")
	}
}
