package subject

import "testing"

func TestSystemFolderName(t *testing.T) {
	tests := []struct {
		code   System
		want   string
		wantOK bool
	}{
		{SystemLabs, "Лабораторные", true},
		{SystemMaterials, "Материалы", true},
		{SystemHomework, "ДЗ", true},
		{"", "", false},
		{"semester", "", false},
	}
	for _, tt := range tests {
		got, ok := SystemFolderName(tt.code)
		if got.String() != tt.want || ok != tt.wantOK {
			t.Errorf("SystemFolderName(%q) = %q, %v; want %q, %v", tt.code, got, ok, tt.want, tt.wantOK)
		}
	}
}
