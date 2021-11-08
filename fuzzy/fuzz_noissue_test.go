package fuzzy

import (
	"fmt"
	"testing"
	"time"
)

func TestIBFT_NoIssue(t *testing.T) {

	/*
		tracer := otel.Tracer("test-tracer")

		// labels represent additional key-value descriptors that can be bound to a
		// metric observer or recorder.
		commonLabels := []attribute.KeyValue{
			attribute.String("labelA", "chocolate"),
			attribute.String("labelB", "raspberry"),
			attribute.String("labelC", "vanilla"),
		}

		// work begins
		ctx, span := tracer.Start(
			context.Background(),
			"CollectorExporter-Example",
			trace.WithAttributes(commonLabels...))
		defer span.End()
		for i := 0; i < 10; i++ {
			_, iSpan := tracer.Start(ctx, fmt.Sprintf("Sample-%d", i))
			log.Printf("Doing really hard work (%d / 10)\n", i+1)

			<-time.After(time.Second)
			iSpan.End()
		}

		log.Printf("Done!")

		return
	*/

	c := newIBFTCluster(t, "noissue", 5)
	c.Start()

	time.Sleep(10 * time.Second)
	c.Stop()

	fmt.Println(c)
}
