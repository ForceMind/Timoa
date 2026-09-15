package money

import "testing"

func TestParseYuan(t *testing.T) {
	cases := []struct {
		in   string
		want int64
		err  error
	}{
		{"12.34", 1234, nil},
		{"0.01", 1, nil},
		{"500", 50000, nil},
		{"28", 2800, nil},
		{".5", 50, nil},
		{"3.5", 350, nil},
		{" 12.34 ", 1234, nil},
		{"0", 0, nil},
		{"0.00", 0, nil},
		{"0007.20", 720, nil},
		// rejections
		{"", 0, ErrInvalid},
		{"abc", 0, ErrInvalid},
		{"1.234", 0, ErrInvalid},
		{"-5", 0, ErrInvalid},
		{"1.2.3", 0, ErrInvalid},
		{"1e3", 0, ErrInvalid},
		{"12,34", 0, ErrInvalid},
		{"99999999999999999", 0, ErrRange},
	}
	for _, c := range cases {
		got, err := ParseYuan(c.in)
		if err != c.err {
			t.Errorf("ParseYuan(%q): err=%v want %v", c.in, err, c.err)
			continue
		}
		if err == nil && got != c.want {
			t.Errorf("ParseYuan(%q)=%d want %d", c.in, got, c.want)
		}
	}
}

func TestParseYuanRequiredRejectsZero(t *testing.T) {
	if _, err := ParseYuanRequired("0"); err != ErrZero {
		t.Fatalf("want ErrZero, got %v", err)
	}
	if _, err := ParseYuanRequired("0.00"); err != ErrZero {
		t.Fatalf("want ErrZero for 0.00, got %v", err)
	}
}

func TestFormatCents(t *testing.T) {
	if got := FormatCents(1234); got != "12.34" {
		t.Errorf("got %q", got)
	}
	if got := FormatCents(-31500); got != "-315.00" {
		t.Errorf("got %q", got)
	}
	if got := FormatCents(5); got != "0.05" {
		t.Errorf("got %q", got)
	}
}

func TestCheckedAddOverflow(t *testing.T) {
	if _, err := CheckedAdd(1<<62, 1<<62); err != ErrRange {
		t.Fatalf("want overflow ErrRange, got %v", err)
	}
	if got, err := CheckedAdd(100, -40); err != nil || got != 60 {
		t.Fatalf("got %d err %v", got, err)
	}
}
