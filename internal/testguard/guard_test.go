package testguard_test

import (
	"context"
	"strings"
	"testing"

	"github.com/Buffer379x/jdweb/internal/jd"
	"github.com/Buffer379x/jdweb/internal/testguard"
	"github.com/Buffer379x/jdweb/internal/testjd"
)

const prefix = "jdweb-it-"

func TestRequireRefusesWhatIsNotTheTestStack(t *testing.T) {
	ctx := context.Background()
	t.Setenv("JDWEB_TEST_JD_CONTAINER", "")
	t.Setenv("JDWEB_TEST_ALLOW_REMOTE", "")

	t.Run("fake without the marker", func(t *testing.T) {
		f := testjd.New(t)
		err := testguard.Require(ctx, jd.New(f.URL), f.URL, prefix)
		if err == nil || !strings.Contains(err.Error(), "device name") {
			t.Fatalf("err = %v, want a refusal about the device name", err)
		}
	})
	t.Run("marker set", func(t *testing.T) {
		f := testjd.New(t)
		f.SetConfig(testguard.MarkerInterface, testguard.MarkerKey, testguard.MarkerValue)
		if err := testguard.Require(ctx, jd.New(f.URL), f.URL, prefix); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("foreign package", func(t *testing.T) {
		f := testjd.New(t)
		f.SetConfig(testguard.MarkerInterface, testguard.MarkerKey, testguard.MarkerValue)
		f.AddPackage(testjd.Linkgrabber, testjd.Package{Name: "my movies"}, testjd.Link{Name: "a"})
		if err := testguard.Require(ctx, jd.New(f.URL), f.URL, prefix); err == nil || !strings.Contains(err.Error(), "my movies") {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("open dialog", func(t *testing.T) {
		f := testjd.New(t)
		f.SetConfig(testguard.MarkerInterface, testguard.MarkerKey, testguard.MarkerValue)
		f.AddDialog(testjd.Dialog{})
		if err := testguard.Require(ctx, jd.New(f.URL), f.URL, prefix); err == nil || !strings.Contains(err.Error(), "dialog") {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("foreign host", func(t *testing.T) {
		err := testguard.Require(ctx, jd.New("http://192.0.2.1:3128"), "http://192.0.2.1:3128", prefix)
		if err == nil || !strings.Contains(err.Error(), "host") {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("remote needs the exact url", func(t *testing.T) {
		t.Setenv("JDWEB_TEST_ALLOW_REMOTE", "http://192.0.2.9:3128")
		err := testguard.Require(ctx, jd.New("http://192.0.2.1:3128"), "http://192.0.2.1:3128", prefix)
		if err == nil || !strings.Contains(err.Error(), "host") {
			t.Fatalf("err = %v", err)
		}
	})
}
