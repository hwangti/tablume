package dbx

import "testing"

// MySQL/MariaDB BIT is exported as its number, not its bytes.
func TestExportBit(t *testing.T) {
	if s, k := exportValue([]byte{1}, "BIT"); s != "1" || k != vNum {
		t.Errorf("b'1': %q %v", s, k)
	}
	if s, k := exportValue([]byte{2, 2}, "BIT"); s != "514" || k != vNum {
		t.Errorf("b'1000000010': %q %v", s, k)
	}
	if s := sqlLiteral("1", vNum, "mysql"); s != "1" {
		t.Errorf("literal %q", s)
	}
}
