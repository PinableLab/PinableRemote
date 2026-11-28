//go:build windows && cgo && pinable_freerdp

package main

import (
	"bufio"
	"strings"
	"testing"
)

func TestEnrollmentOnlyConsumesFirstLine(t *testing.T) {
	r := bufio.NewReaderSize(strings.NewReader(`{"host":"qa.example","port":3389,"domain":"QA","user":"reader","ca_pem":"Y2E="}
{"id":1,"op":"open"}
`), bootstrapLimit+1)
	v, err := readEnrollment(r)
	if err != nil || v.Host != "qa.example" || string(v.CAPEM) != "ca" {
		t.Fatal("enrollment not decoded")
	}
	if next, err := r.ReadString('\n'); err != nil || next != "{\"id\":1,\"op\":\"open\"}\n" {
		t.Fatal("broker frame lost")
	}
}

func TestEnrollmentRejectsSecretsAndOversize(t *testing.T) {
	for _, line := range []string{
		`{"host":"qa.example","port":3389,"domain":"QA","user":"reader","ca_pem":"Y2E=","password":"unused"}` + "\n",
		`{"host":"qa.example","port":3389,"domain":"QA","user":"reader","ca_pem":"Y2E="} {}` + "\n",
		strings.Repeat(" ", bootstrapLimit+1) + "\n",
	} {
		if _, err := readEnrollment(bufio.NewReaderSize(strings.NewReader(line), bootstrapLimit+1)); err == nil {
			t.Fatal("invalid bootstrap accepted")
		}
	}
}
