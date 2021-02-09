package tftest

import (
	"testing"
)

func TestTFTest(t *testing.T) {
	tf := New(t)
	tf.HandleSignals(true)
	tf.Apply("testdata/docker-test.tf")

	if len(tf.State()) == 0 {
		t.Fatal("state was not parsed")
	}
}
