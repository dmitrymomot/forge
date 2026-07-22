// Package s3 backs data/objectstore with an S3-compatible bucket via
// aws-sdk-go-v2. It implements objectstore.Store and objectstore.URLSigner
// (presigned GET/PUT), works against AWS S3, MinIO, and other S3-compatible
// services, and holds the module's only aws-sdk dependency.
//
// # Usage
//
//	cfg, err := config.LoadDefaultConfig(ctx) // aws-sdk-go-v2/config
//	if err != nil {
//		log.Fatal(err)
//	}
//	store := s3.New(awss3.NewFromConfig(cfg), "my-uploads")
//
//	bucket, err := objectstore.New(store,
//		objectstore.WithAllowedTypes("image/png", "image/jpeg"),
//	)
//	if err != nil {
//		log.Fatal(err)
//	}
//
//	url, err := bucket.SignedGetURL(ctx, "avatars/u123.png", 15*time.Minute)
package s3
