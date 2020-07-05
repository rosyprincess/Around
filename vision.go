package main

import (
	"context"
	"fmt"

	vision "cloud.google.com/go/vision/apiv1" // vision is alias (import as)
)

// Annotate an image file based on Cloud Vision API, return score and error if exists.
// uri -> GCS internal link, return dectect result and err
func annotate(uri string) (float32, error) {
	ctx := context.Background()
	client, err := vision.NewImageAnnotatorClient(ctx)
	if err != nil {
		return 0.0, err
	}
	defer client.Close()

	image := vision.NewImageFromURI(uri)
	annotations, err := client.DetectFaces(ctx, image, nil, 1) // detect 1 face only
	if err != nil {
		return 0.0, err
	}

	if len(annotations) == 0 { //detect no face -> array length is 0
		fmt.Println("No faces found.")
		return 0.0, nil
	}
	// detected face: array length is 1(we only detect one face)
	return annotations[0].DetectionConfidence, nil
}
