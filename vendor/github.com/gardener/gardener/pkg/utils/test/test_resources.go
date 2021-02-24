// Copyright (c) 2021 SAP SE or an SAP affiliate company. All rights reserved. This file is licensed under the Apache Software License, v. 2 except as noted otherwise in the LICENSE file
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package test

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/apimachinery/pkg/util/sets"
	k8syaml "k8s.io/apimachinery/pkg/util/yaml"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// EnsureTestResources reads test resources from path, applies them using the given client and returns the created
// objects.
func EnsureTestResources(ctx context.Context, c client.Client, path string) ([]client.Object, error) {
	objects, err := ReadTestResources(c.Scheme(), path)
	if err != nil {
		return nil, fmt.Errorf("error decoding resources: %w", err)
	}

	for _, obj := range objects {
		current := obj.DeepCopyObject().(client.Object)
		if err := c.Get(ctx, client.ObjectKeyFromObject(current), current); err != nil {
			if !apierrors.IsNotFound(err) {
				return nil, err
			}

			// object doesn't exists, create it
			if err := c.Create(ctx, obj); err != nil {
				return nil, err
			}
		} else {
			// object already exists, update it
			if err := c.Patch(ctx, obj, client.MergeFromWithOptions(current, client.MergeFromWithOptimisticLock{})); err != nil {
				return nil, err
			}
		}
	}
	return objects, nil
}

// ReadTestResources reads test resources from path, decodes them using the given scheme and returns the parsed objects.
// Objects are values of the proper API types, if registered in the given scheme, and *unstructured.Unstructured
// otherwise.
func ReadTestResources(scheme *runtime.Scheme, path string) ([]client.Object, error) {
	decoder := serializer.NewCodecFactory(scheme).UniversalDeserializer()

	var files []os.FileInfo
	var err error
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		path, files = filepath.Dir(path), []os.FileInfo{info}
	} else {
		if files, err = ioutil.ReadDir(path); err != nil {
			return nil, err
		}
	}

	// file extensions that may contain Webhooks
	resourceExtensions := sets.NewString(".json", ".yaml", ".yml")

	var objects []client.Object
	for _, file := range files {
		// Only parse allowlisted file types
		if !resourceExtensions.Has(filepath.Ext(file.Name())) {
			continue
		}

		// Unmarshal Webhooks from file into structs
		docs, err := readDocuments(filepath.Join(path, file.Name()))
		if err != nil {
			return nil, err
		}

		for _, doc := range docs {
			obj, err := runtime.Decode(decoder, doc)
			if err != nil {
				return nil, err
			}
			clientObj, ok := obj.(client.Object)
			if !ok {
				return nil, fmt.Errorf("%T does not implement client.Object", obj)
			}

			objects = append(objects, clientObj)
		}
	}
	return objects, nil

}

// readDocuments reads documents from file
func readDocuments(fp string) ([][]byte, error) {
	b, err := ioutil.ReadFile(fp)
	if err != nil {
		return nil, err
	}

	var docs [][]byte
	reader := k8syaml.NewYAMLReader(bufio.NewReader(bytes.NewReader(b)))
	for {
		// Read document
		doc, err := reader.Read()
		if err != nil {
			if err == io.EOF {
				break
			}

			return nil, err
		}

		docs = append(docs, doc)
	}

	return docs, nil
}
