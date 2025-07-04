/*
Copyright 2021 The Fluid Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package alluxio

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	. "github.com/agiledragon/gomonkey/v2"
	datav1alpha1 "github.com/fluid-cloudnative/fluid/api/v1alpha1"
	"github.com/fluid-cloudnative/fluid/pkg/ddc/alluxio/operations"
	"github.com/fluid-cloudnative/fluid/pkg/ddc/base"
	"github.com/fluid-cloudnative/fluid/pkg/utils"
	"github.com/fluid-cloudnative/fluid/pkg/utils/fake"
	"github.com/fluid-cloudnative/fluid/pkg/utils/kubeclient"
	"github.com/go-logr/logr"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func mockExecCommandInContainerForTotalStorageBytes() (stdout string, stderr string, err error) {
	r := `File Count               Folder Count             Folder Size
	50000                    1000                     6706560319`
	return r, "", nil
}

func mockExecCommandInContainerForTotalFileNums() (stdout string, stderr string, err error) {
	r := `Master.FilesCompleted  (Type: COUNTER, Value: 1,331,167)`
	return r, "", nil
}

// TestUsedStorageBytes tests the UsedStorageBytes method of the AlluxioEngine.
// It verifies that the method returns the expected used storage value and error status.
// Currently, it checks a basic case where the expected used storage is 0 and no error is expected.
func TestUsedStorageBytes(t *testing.T) {
	type fields struct {
	}
	tests := []struct {
		name      string
		fields    fields
		wantValue int64
		wantErr   bool
	}{
		{
			name:      "test",
			fields:    fields{},
			wantValue: 0,
			wantErr:   false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := &AlluxioEngine{}
			gotValue, err := e.UsedStorageBytes()
			if (err != nil) != tt.wantErr {
				t.Errorf("AlluxioEngine.UsedStorageBytes() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if gotValue != tt.wantValue {
				t.Errorf("AlluxioEngine.UsedStorageBytes() = %v, want %v", gotValue, tt.wantValue)
			}
		})
	}
}

// TestFreeStorageBytes is a unit test for the AlluxioEngine.FreeStorageBytes method.
// This test function defines a set of test cases, each including the expected return value and a flag indicating whether an error is expected.
// The test invokes the FreeStorageBytes method and checks whether the returned value matches the expected result and whether error handling is performed correctly.
func TestFreeStorageBytes(t *testing.T) {
	type fields struct {
	}
	tests := []struct {
		name      string
		fields    fields
		wantValue int64
		wantErr   bool
	}{
		{
			name:      "test",
			fields:    fields{},
			wantValue: 0,
			wantErr:   false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := &AlluxioEngine{}
			gotValue, err := e.FreeStorageBytes()
			if (err != nil) != tt.wantErr {
				t.Errorf("AlluxioEngine.FreeStorageBytes() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if gotValue != tt.wantValue {
				t.Errorf("AlluxioEngine.FreeStorageBytes() = %v, want %v", gotValue, tt.wantValue)
			}
		})
	}
}

// TestTotalStorageBytes verifies the functionality of AlluxioEngine's TotalStorageBytes method.
// It validates whether the method correctly calculates total storage capacity by:
// - Mocking AlluxioRuntime configuration and container command execution
// - Testing both normal scenarios (expected values) and error conditions
// - Using patched container command output to ensure predictable test results
// Each test case checks if returned values match expectations and errors are properly handled.
func TestTotalStorageBytes(t *testing.T) {
	type fields struct {
		runtime *datav1alpha1.AlluxioRuntime
		name    string
	}
	tests := []struct {
		name      string
		fields    fields
		wantValue int64
		wantErr   bool
	}{
		{
			name: "test",
			fields: fields{
				runtime: &datav1alpha1.AlluxioRuntime{
					ObjectMeta: v1.ObjectMeta{
						Name: "spark",
					},
				},
			},
			wantValue: 6706560319,
			wantErr:   false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := &AlluxioEngine{
				runtime: tt.fields.runtime,
				name:    tt.fields.name,
			}
			patch1 := ApplyFunc(kubeclient.ExecCommandInContainerWithFullOutput, func(ctx context.Context, podName string, containerName string, namespace string, cmd []string) (string, string, error) {
				stdout, stderr, err := mockExecCommandInContainerForTotalStorageBytes()
				return stdout, stderr, err
			})
			defer patch1.Reset()
			gotValue, err := e.TotalStorageBytes()
			if (err != nil) != tt.wantErr {
				t.Errorf("AlluxioEngine.TotalStorageBytes() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if gotValue != tt.wantValue {
				t.Errorf("AlluxioEngine.TotalStorageBytes() = %v, want %v", gotValue, tt.wantValue)
			}
		})
	}
}

// TestTotalFileNums validates the AlluxioEngine's ability to correctly retrieve total file numbers from the Alluxio runtime.
// The test performs the following operations:
// - Creates mock AlluxioRuntime configurations
// - Overrides Kubernetes exec command interactions
// - Verifies both value accuracy and error handling
//
// Test Components:
// - fields: Contains the Alluxio runtime configuration and engine identity
// - tests: Table-driven test cases with expected values and error conditions
// !
// Flow:
// 1. Initialize AlluxioEngine with test parameters
// 2. Mock Kubernetes command execution using function patch
// 3. Execute TotalFileNums() method
// 4. Validate against expected values and error states
//
// Note:
// - Uses monkey patching for Kubernetes client isolation
// - Requires proper setup of mockExecCommandInContainerForTotalFileNums
func TestTotalFileNums(t *testing.T) {
	type fields struct {
		runtime *datav1alpha1.AlluxioRuntime
		name    string
	}
	tests := []struct {
		name      string
		fields    fields
		wantValue int64
		wantErr   bool
	}{
		{
			name: "test",
			fields: fields{
				runtime: &datav1alpha1.AlluxioRuntime{
					ObjectMeta: v1.ObjectMeta{
						Name: "spark",
					},
				},
			},
			wantValue: 1331167,
			wantErr:   false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := &AlluxioEngine{
				runtime: tt.fields.runtime,
				name:    tt.fields.name,
			}
			patch1 := ApplyFunc(kubeclient.ExecCommandInContainerWithFullOutput, func(ctx context.Context, podName string, containerName string, namespace string, cmd []string) (string, string, error) {
				stdout, stderr, err := mockExecCommandInContainerForTotalFileNums()
				return stdout, stderr, err
			})
			defer patch1.Reset()
			gotValue, err := e.TotalFileNums()
			if (err != nil) != tt.wantErr {
				t.Errorf("AlluxioEngine.TotalFileNums() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if gotValue != tt.wantValue {
				t.Errorf("AlluxioEngine.TotalFileNums() = %v, want %v", gotValue, tt.wantValue)
			}
		})
	}
}

func TestShouldCheckUFS(t *testing.T) {
	tests := []struct {
		name       string
		wantShould bool
		wantErr    bool
	}{
		{
			name:       "test",
			wantShould: true,
			wantErr:    false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := &AlluxioEngine{}
			gotShould, err := e.ShouldCheckUFS()
			if (err != nil) != tt.wantErr {
				t.Errorf("AlluxioEngine.ShouldCheckUFS() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if gotShould != tt.wantShould {
				t.Errorf("AlluxioEngine.ShouldCheckUFS() = %v, want %v", gotShould, tt.wantShould)
			}
		})
	}
}

// TestPrepareUFS tests the PrepareUFS method of AlluxioEngine.
// This method prepares the underlying file system (UFS) by checking
// the Alluxio master state, mounting UFS, and performing necessary
// metadata synchronization.
//
// Test logic:
//  1. Create multiple test cases to simulate different states of
//     AlluxioRuntime, Dataset, and StatefulSet.
//  2. Initialize AlluxioEngine and its dependencies using a fake client.
//  3. Use Monkey Patching to mock the behavior of AlluxioFileUtils methods.
//  4. Call e.PrepareUFS() and verify whether the UFS mounting process
//     executes correctly.
//  5. Assert that the returned errors match the expected outcomes.
//
// Parameters:
// - t *testing.T: The testing context provided by the Go testing framework.
func TestPrepareUFS(t *testing.T) {
	type fields struct {
		runtime            *datav1alpha1.AlluxioRuntime
		dataset            *datav1alpha1.Dataset
		name               string
		namespace          string
		Log                logr.Logger
		MetadataSyncDoneCh chan base.MetadataSyncResult
		master             *appsv1.StatefulSet
	}
	tests := []struct {
		name    string
		fields  fields
		wantErr bool
	}{
		{
			name: "test",
			fields: fields{
				runtime: &datav1alpha1.AlluxioRuntime{
					ObjectMeta: v1.ObjectMeta{
						Name:      "spark",
						Namespace: "default",
					},
				},
				master: &appsv1.StatefulSet{
					ObjectMeta: v1.ObjectMeta{
						Name:      "hbase-master",
						Namespace: "fluid",
					},
					Spec: appsv1.StatefulSetSpec{
						Replicas: ptr.To[int32](2),
					},
					Status: appsv1.StatefulSetStatus{
						Replicas:      3,
						ReadyReplicas: 2,
					},
				},
				dataset: &datav1alpha1.Dataset{
					ObjectMeta: v1.ObjectMeta{
						Name:      "spark",
						Namespace: "default",
					},
					Spec: datav1alpha1.DatasetSpec{
						Mounts: []datav1alpha1.Mount{
							{
								MountPoint: "cosn://imagenet-1234567/",
							},
						},
						DataRestoreLocation: &datav1alpha1.DataRestoreLocation{
							Path:     "local:///tmp/restore",
							NodeName: "192.168.0.1",
						},
					},
					Status: datav1alpha1.DatasetStatus{
						UfsTotal: "",
					},
				},
				name:      "spark",
				namespace: "default",
				Log:       fake.NullLogger(),
			},
			wantErr: false,
		},
		{
			name: "ha master",
			fields: fields{
				runtime: &datav1alpha1.AlluxioRuntime{
					ObjectMeta: v1.ObjectMeta{
						Name:      "spark",
						Namespace: "default",
					},
					Spec: datav1alpha1.AlluxioRuntimeSpec{
						Master: datav1alpha1.AlluxioCompTemplateSpec{
							Replicas: 3,
						},
					},
				},
				master: &appsv1.StatefulSet{
					ObjectMeta: v1.ObjectMeta{
						Name:      "hbase-master",
						Namespace: "fluid",
					},
					Spec: appsv1.StatefulSetSpec{
						Replicas: ptr.To[int32](2),
					},
					Status: appsv1.StatefulSetStatus{
						Replicas:      3,
						ReadyReplicas: 2,
					},
				},
				dataset: &datav1alpha1.Dataset{
					ObjectMeta: v1.ObjectMeta{
						Name:      "spark",
						Namespace: "default",
					},
					Spec: datav1alpha1.DatasetSpec{
						Mounts: []datav1alpha1.Mount{
							{
								MountPoint: "cosn://imagenet-1234567/",
							},
						},
						DataRestoreLocation: &datav1alpha1.DataRestoreLocation{
							Path:     "local:///tmp/restore",
							NodeName: "192.168.0.1",
						},
					},
					Status: datav1alpha1.DatasetStatus{
						UfsTotal: "",
					},
				},
				name:      "spark",
				namespace: "default",
				Log:       fake.NullLogger(),
			},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := runtime.NewScheme()
			s.AddKnownTypes(datav1alpha1.GroupVersion, tt.fields.runtime)
			s.AddKnownTypes(datav1alpha1.GroupVersion, tt.fields.dataset)
			s.AddKnownTypes(datav1alpha1.GroupVersion, tt.fields.master)
			_ = corev1.AddToScheme(s)
			mockClient := fake.NewFakeClientWithScheme(s, tt.fields.runtime, tt.fields.dataset, tt.fields.master)

			var afsUtils operations.AlluxioFileUtils
			patch1 := ApplyMethod(reflect.TypeOf(afsUtils), "Ready", func(_ operations.AlluxioFileUtils) bool {
				return true
			})
			defer patch1.Reset()

			patch2 := ApplyMethod(reflect.TypeOf(afsUtils), "IsMounted", func(_ operations.AlluxioFileUtils, AlluxioPath string) (bool, error) {
				return false, nil
			})
			defer patch2.Reset()

			patch3 := ApplyMethod(reflect.TypeOf(afsUtils), "Mount", func(_ operations.AlluxioFileUtils, alluxioPath string, ufsPath string, options map[string]string, readOnly bool, shared bool) error {
				return nil
			})
			defer patch3.Reset()

			patch4 := ApplyMethod(reflect.TypeOf(afsUtils), "QueryMetaDataInfoIntoFile", func(_ operations.AlluxioFileUtils, key operations.KeyOfMetaDataFile, filename string) (string, error) {
				return "10000", nil
			})
			defer patch4.Reset()

			patch5 := ApplyMethod(reflect.TypeOf(afsUtils), "ExecMountScripts", func(_ operations.AlluxioFileUtils) error {
				return nil
			})
			defer patch5.Reset()

			e := &AlluxioEngine{
				runtime:            tt.fields.runtime,
				name:               tt.fields.name,
				namespace:          tt.fields.namespace,
				Log:                tt.fields.Log,
				Client:             mockClient,
				MetadataSyncDoneCh: tt.fields.MetadataSyncDoneCh,
			}
			if err := e.PrepareUFS(); (err != nil) != tt.wantErr {
				t.Errorf("AlluxioEngine.PrepareUFS() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// UpdateDatasetStatus updates the status of a dataset in the JindoEngine.
// This function synchronizes the dataset phase with the underlying runtime status.
//
// Parameters:
// - phase (datav1alpha1.DatasetPhase): The target phase to transition to.
//
// Returns:
// - error: Returns nil on success, or error details if the update fails.
func TestGenUFSMountOptions(t *testing.T) {
	type fields struct {
		runtime            *datav1alpha1.AlluxioRuntime
		dataset            *datav1alpha1.Dataset
		secret             *corev1.Secret
		name               string
		namespace          string
		Log                logr.Logger
		MetadataSyncDoneCh chan base.MetadataSyncResult
	}
	tests := []struct {
		name        string
		fields      fields
		wantOptions map[string]string
	}{
		{
			name: "test",
			fields: fields{
				name:      "spark",
				namespace: "default",
				Log:       fake.NullLogger(),
				secret: &corev1.Secret{
					ObjectMeta: v1.ObjectMeta{
						Name:      "spark",
						Namespace: "default",
					},
					Data: map[string][]byte{
						"key1": []byte("value1"),
						"key2": []byte("value2"),
					},
				},
				runtime: &datav1alpha1.AlluxioRuntime{},
				dataset: &datav1alpha1.Dataset{
					ObjectMeta: v1.ObjectMeta{
						Name:      "spark",
						Namespace: "default",
					},
					Spec: datav1alpha1.DatasetSpec{
						SharedOptions: map[string]string{
							"test2": "test2",
						},
						SharedEncryptOptions: []datav1alpha1.EncryptOption{
							{
								Name: "testEncrypt1",
								ValueFrom: datav1alpha1.EncryptOptionSource{SecretKeyRef: datav1alpha1.SecretKeySelector{
									Name: "spark",
									Key:  "key2",
								}},
							},
						},
						Mounts: []datav1alpha1.Mount{
							{
								MountPoint: "cosn://imagenet-1234567/",
								Options: map[string]string{
									"test1": "test1",
								},
								EncryptOptions: []datav1alpha1.EncryptOption{
									{
										Name: "testEncrypt",
										ValueFrom: datav1alpha1.EncryptOptionSource{SecretKeyRef: datav1alpha1.SecretKeySelector{
											Name: "spark",
											Key:  "key1",
										}},
									},
								},
							},
						},
						DataRestoreLocation: &datav1alpha1.DataRestoreLocation{
							Path:     "local:///tmp/restore",
							NodeName: "192.168.0.1",
						},
					},
					Status: datav1alpha1.DatasetStatus{
						UfsTotal: "",
					},
				},
			},
			wantOptions: map[string]string{
				"test1":        "test1",
				"test2":        "test2",
				"testEncrypt":  "value1",
				"testEncrypt1": "value2",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := runtime.NewScheme()
			s.AddKnownTypes(datav1alpha1.GroupVersion, tt.fields.runtime)
			s.AddKnownTypes(datav1alpha1.GroupVersion, tt.fields.dataset)
			_ = corev1.AddToScheme(s)
			mockClient := fake.NewFakeClientWithScheme(s, tt.fields.runtime, tt.fields.dataset, tt.fields.secret)

			var afsUtils operations.AlluxioFileUtils
			patch1 := ApplyMethod(reflect.TypeOf(afsUtils), "Ready", func(_ operations.AlluxioFileUtils) bool {
				return true
			})
			defer patch1.Reset()

			patch2 := ApplyMethod(reflect.TypeOf(afsUtils), "IsMounted", func(_ operations.AlluxioFileUtils, AlluxioPath string) (bool, error) {
				return false, nil
			})
			defer patch2.Reset()

			patch3 := ApplyMethod(reflect.TypeOf(afsUtils), "Mount", func(_ operations.AlluxioFileUtils, alluxioPath string, ufsPath string, options map[string]string, readOnly bool, shared bool) error {
				return nil
			})
			defer patch3.Reset()

			patch4 := ApplyMethod(reflect.TypeOf(afsUtils), "QueryMetaDataInfoIntoFile", func(_ operations.AlluxioFileUtils, key operations.KeyOfMetaDataFile, filename string) (string, error) {
				return "10000", nil
			})
			defer patch4.Reset()

			e := &AlluxioEngine{
				runtime:            tt.fields.runtime,
				name:               tt.fields.name,
				namespace:          tt.fields.namespace,
				Log:                tt.fields.Log,
				Client:             mockClient,
				MetadataSyncDoneCh: tt.fields.MetadataSyncDoneCh,
			}
			getoptions, err := e.genUFSMountOptions(tt.fields.dataset.Spec.Mounts[0], tt.fields.dataset.Spec.SharedOptions, tt.fields.dataset.Spec.SharedEncryptOptions, true)
			if err != nil {
				t.Errorf("AlluxioEngine.genUFSMountOptions() error = %v", err)
			}
			for k, v := range getoptions {
				if v1, ok := tt.wantOptions[k]; !ok {
					t.Errorf("AlluxioEngine.genUFSMountOptions() should has key: %v", k)
				} else {
					if v1 != v {
						t.Errorf("AlluxioEngine.genUFSMountOptions()  key: %v value: %v, get value: %v", k, v1, v)
					} else {
						delete(tt.wantOptions, k)
					}
				}
			}

			if len(tt.wantOptions) > 0 {
				t.Errorf("AlluxioEngine.genUFSMountOptions() not equal, wantOptions: %v", tt.wantOptions)
			}
		})
	}
}

// TestGenUFSMountOptionsMultiTimes verifies the behavior when generating Under FileSystem (UFS) mount options
// multiple times. It ensures that shared configuration options and encrypted credentials from Kubernetes Secrets
// are properly merged with individual mount point configurations. The test specifically checks that:
// - Shared options defined at the dataset level are correctly applied to all mounts
// - Encrypted parameters (e.g., AWS credentials) are properly extracted from Secrets
// - Multiple consecutive calls to genUFSMountOptions maintain consistency and don't overwrite shared configurations
// - Both regular options and secret-based options are combined in the final output
// This validation is crucial for multi-mount scenarios to prevent configuration conflicts between mount points.
func TestGenUFSMountOptionsMultiTimes(t *testing.T) {
	type fields struct {
		dataset               datav1alpha1.Dataset
		extractEncryptOptions bool
	}
	tests := []struct {
		name       string
		fields     fields
		wantErr    bool
		wantValue1 map[string]string
		wantValue2 map[string]string
	}{
		{
			name: "genUFSMountTwiceWithSharedOptions",
			fields: fields{
				dataset: datav1alpha1.Dataset{
					Spec: datav1alpha1.DatasetSpec{
						Mounts: []datav1alpha1.Mount{
							{
								MountPoint: "s3://test1",
								Name:       "test1",
							},
							{
								MountPoint: "s3://test2",
								Name:       "test2",
							},
						},
						SharedOptions: map[string]string{
							"alluxio.underfs.s3.endpoint":            "http://10.10.10.10:32000",
							"alluxio.underfs.s3.disable.dns.buckets": "true",
							"alluxio.underfs.s3.inherit.acl":         "false",
						},
						SharedEncryptOptions: []datav1alpha1.EncryptOption{
							{
								Name: "aws.accessKeyId",
								ValueFrom: datav1alpha1.EncryptOptionSource{
									SecretKeyRef: datav1alpha1.SecretKeySelector{
										Name: "minio",
										Key:  "accessKeyId",
									},
								},
							},
							{
								Name: "aws.secretKey",
								ValueFrom: datav1alpha1.EncryptOptionSource{
									SecretKeyRef: datav1alpha1.SecretKeySelector{
										Name: "minio",
										Key:  "secretKey",
									},
								},
							},
						},
					},
				},
				extractEncryptOptions: true,
			},
			wantValue1: map[string]string{
				"alluxio.underfs.s3.endpoint":            "http://10.10.10.10:32000",
				"alluxio.underfs.s3.disable.dns.buckets": "true",
				"alluxio.underfs.s3.inherit.acl":         "false",
				"aws.accessKeyId":                        "minioadmin",
				"aws.secretKey":                          "minioadmin",
			},
			wantValue2: map[string]string{
				"alluxio.underfs.s3.endpoint":            "http://10.10.10.10:32000",
				"alluxio.underfs.s3.disable.dns.buckets": "true",
				"alluxio.underfs.s3.inherit.acl":         "false",
				"aws.accessKeyId":                        "minioadmin",
				"aws.secretKey":                          "minioadmin",
			},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := &AlluxioEngine{}
			patch := ApplyFunc(kubeclient.GetSecret, func(client client.Client, name, namespace string) (*corev1.Secret, error) {
				return &corev1.Secret{
					Data: map[string][]byte{
						"accessKeyId": []byte("minioadmin"),
						"secretKey":   []byte("minioadmin"),
					},
				}, nil
			})
			defer patch.Reset()
			gotValue1, err1 := e.genUFSMountOptions(
				tt.fields.dataset.Spec.Mounts[0],
				tt.fields.dataset.Spec.SharedOptions,
				tt.fields.dataset.Spec.SharedEncryptOptions,
				tt.fields.extractEncryptOptions,
			)
			gotValue2, err2 := e.genUFSMountOptions(
				tt.fields.dataset.Spec.Mounts[1],
				tt.fields.dataset.Spec.SharedOptions,
				tt.fields.dataset.Spec.SharedEncryptOptions,
				tt.fields.extractEncryptOptions,
			)
			if ((err1 != nil) != tt.wantErr) || ((err2 != nil) != tt.wantErr) {
				t.Errorf("Call AlluxioEngine.genUFSMountOptions() twice, first error = %v, second error = %v", err1, err2)
				return
			}

			for k, v := range gotValue1 {
				if v1, ok := tt.wantValue1[k]; !ok {
					t.Errorf("Call AlluxioEngine.genUFSMountOptions() firstly, shouldn't have key: %v", k)
				} else {
					if v1 != v {
						t.Errorf("Call AlluxioEngine.genUFSMountOptions() firstly, key: %v value: %v, get value: %v", k, v1, v)
					} else {
						delete(tt.wantValue1, k)
					}
				}
			}

			if len(tt.wantValue1) > 0 {
				t.Errorf("Call AlluxioEngine.genUFSMountOptions() firstly, number of options not equal, wantOptions: %v", tt.wantValue1)
			}

			for k, v := range gotValue2 {
				if v1, ok := tt.wantValue2[k]; !ok {
					t.Errorf("Call AlluxioEngine.genUFSMountOptions() secondly, shouldn't have key: %v", k)
				} else {
					if v1 != v {
						t.Errorf("Call AlluxioEngine.genUFSMountOptions() secondly, key: %v value: %v, get value: %v", k, v1, v)
					} else {
						delete(tt.wantValue2, k)
					}
				}
			}

			if len(tt.wantValue2) > 0 {
				t.Errorf("Call AlluxioEngine.genUFSMountOptions() secondly, number of options not equal, wantOptions: %v", tt.wantValue1)
			}
		})
	}
}

// TestGenUFSMountOptionsWithDuplicatedKey is a unit test for the genUFSMountOptions method
// of the AlluxioEngine struct. This test verifies the handling of duplicated keys in mount options,
// particularly in the context of shared encryption options and mount-specific encryption options.
//
// The test sets up a fake Kubernetes client with a dataset, secret, and runtime, then mocks
// the behavior of Alluxio-related methods such as Ready, IsMounted, Mount, and QueryMetaDataInfoIntoFile.
//
// Test Case:
// - A dataset is defined with both shared encryption options and mount-specific encryption options.
// - The secret contains multiple key-value pairs used for encryption references.
// - The test checks whether genUFSMountOptions correctly detects and handles duplicated keys.
// - If an error is expected (wantErr = true), the test verifies that the function returns an error.
//
// Mocks & Patches:
// - Fake Kubernetes client simulates runtime, dataset, and secret objects.
// - Methods from AlluxioFileUtils are patched to control their behavior in the test environment.
//
// Expected Behavior:
// - If duplicated keys exist in mount options, genUFSMountOptions should return an error.
func TestGenUFSMountOptionsWithDuplicatedKey(t *testing.T) {
	type fields struct {
		runtime            *datav1alpha1.AlluxioRuntime
		dataset            *datav1alpha1.Dataset
		secret             *corev1.Secret
		name               string
		namespace          string
		Log                logr.Logger
		MetadataSyncDoneCh chan base.MetadataSyncResult
	}
	tests := []struct {
		name    string
		fields  fields
		wantErr bool
	}{
		{
			name: "test",
			fields: fields{
				name:      "spark",
				namespace: "default",
				Log:       fake.NullLogger(),
				secret: &corev1.Secret{
					ObjectMeta: v1.ObjectMeta{
						Name:      "spark",
						Namespace: "default",
					},
					Data: map[string][]byte{
						"key1": []byte("value1"),
						"key2": []byte("value2"),
					},
				},
				runtime: &datav1alpha1.AlluxioRuntime{},
				dataset: &datav1alpha1.Dataset{
					ObjectMeta: v1.ObjectMeta{
						Name:      "spark",
						Namespace: "default",
					},
					Spec: datav1alpha1.DatasetSpec{
						SharedOptions: map[string]string{
							"test2": "test2",
						},
						SharedEncryptOptions: []datav1alpha1.EncryptOption{
							{
								Name: "testEncrypt1",
								ValueFrom: datav1alpha1.EncryptOptionSource{SecretKeyRef: datav1alpha1.SecretKeySelector{
									Name: "spark",
									Key:  "key2",
								}},
							},
						},
						Mounts: []datav1alpha1.Mount{
							{
								MountPoint: "cosn://imagenet-1234567/",
								Options: map[string]string{
									"test1": "test1",
								},
								EncryptOptions: []datav1alpha1.EncryptOption{
									{
										Name: "test1",
										ValueFrom: datav1alpha1.EncryptOptionSource{SecretKeyRef: datav1alpha1.SecretKeySelector{
											Name: "spark",
											Key:  "key1",
										}},
									},
								},
							},
						},
						DataRestoreLocation: &datav1alpha1.DataRestoreLocation{
							Path:     "local:///tmp/restore",
							NodeName: "192.168.0.1",
						},
					},
					Status: datav1alpha1.DatasetStatus{
						UfsTotal: "",
					},
				},
			},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := runtime.NewScheme()
			s.AddKnownTypes(datav1alpha1.GroupVersion, tt.fields.runtime)
			s.AddKnownTypes(datav1alpha1.GroupVersion, tt.fields.dataset)
			_ = corev1.AddToScheme(s)
			mockClient := fake.NewFakeClientWithScheme(s, tt.fields.runtime, tt.fields.dataset, tt.fields.secret)

			var afsUtils operations.AlluxioFileUtils
			patch1 := ApplyMethod(reflect.TypeOf(afsUtils), "Ready", func(_ operations.AlluxioFileUtils) bool {
				return true
			})
			defer patch1.Reset()

			patch2 := ApplyMethod(reflect.TypeOf(afsUtils), "IsMounted", func(_ operations.AlluxioFileUtils, AlluxioPath string) (bool, error) {
				return false, nil
			})
			defer patch2.Reset()

			patch3 := ApplyMethod(reflect.TypeOf(afsUtils), "Mount", func(_ operations.AlluxioFileUtils, alluxioPath string, ufsPath string, options map[string]string, readOnly bool, shared bool) error {
				return nil
			})
			defer patch3.Reset()

			patch4 := ApplyMethod(reflect.TypeOf(afsUtils), "QueryMetaDataInfoIntoFile", func(_ operations.AlluxioFileUtils, key operations.KeyOfMetaDataFile, filename string) (string, error) {
				return "10000", nil
			})
			defer patch4.Reset()

			e := &AlluxioEngine{
				runtime:            tt.fields.runtime,
				name:               tt.fields.name,
				namespace:          tt.fields.namespace,
				Log:                tt.fields.Log,
				Client:             mockClient,
				MetadataSyncDoneCh: tt.fields.MetadataSyncDoneCh,
			}
			_, err := e.genUFSMountOptions(tt.fields.dataset.Spec.Mounts[0], tt.fields.dataset.Spec.SharedOptions, tt.fields.dataset.Spec.SharedEncryptOptions, true)
			if (err != nil) != tt.wantErr {
				t.Errorf("genUFSMountOptions error = %v, wantErr %v", err, tt.wantErr)
				return
			}

		})
	}
}

// TestFindUnmountedUFS verifies if AlluxioEngine's FindUnmountedUFS method correctly identifies
// UFS paths that should be considered for mounting based on their scheme.
// It iterates through predefined test cases, each with a set of mount points and the expected
// unmounted paths. For each case, it mocks the necessary dependencies, calls FindUnmountedUFS,
// and then compares the returned unmounted paths with the expected ones.
//
// param: t *testing.T - The testing context used for running the test and reporting failures.
//
// returns: None (This is a test function and does not return any value.)
func TestFindUnmountedUFS(t *testing.T) {

	type fields struct {
		mountPoints          []datav1alpha1.Mount
		wantedUnmountedPaths []string
	}

	tests := []fields{
		{
			mountPoints: []datav1alpha1.Mount{
				{
					MountPoint: "s3://bucket/path/train",
					Path:       "/path1",
				},
			},
			wantedUnmountedPaths: []string{"/path1"},
		},
		{
			mountPoints: []datav1alpha1.Mount{
				{
					MountPoint: "local://mnt/test",
					Path:       "/path2",
				},
			},
			wantedUnmountedPaths: []string{},
		},
		{
			mountPoints: []datav1alpha1.Mount{
				{
					MountPoint: "s3://bucket/path/train",
					Path:       "/path1",
				},
				{
					MountPoint: "local://mnt/test",
					Path:       "/path2",
				},
				{
					MountPoint: "hdfs://endpoint/path/train",
					Path:       "/path3",
				},
			},
			wantedUnmountedPaths: []string{"/path1", "/path3"},
		},
	}

	for index, test := range tests {
		t.Run("test", func(t *testing.T) {
			s := runtime.NewScheme()
			runtime := datav1alpha1.AlluxioRuntime{}
			dataset := datav1alpha1.Dataset{
				ObjectMeta: v1.ObjectMeta{
					Name:      "test",
					Namespace: "default",
				},
				Spec: datav1alpha1.DatasetSpec{
					Mounts: test.mountPoints,
				},
			}

			s.AddKnownTypes(datav1alpha1.GroupVersion, &runtime)
			s.AddKnownTypes(datav1alpha1.GroupVersion, &dataset)
			_ = corev1.AddToScheme(s)
			mockClient := fake.NewFakeClientWithScheme(s, &runtime, &dataset)

			var afsUtils operations.AlluxioFileUtils
			patch1 := ApplyMethod(reflect.TypeOf(afsUtils), "Ready", func(_ operations.AlluxioFileUtils) bool {
				return true
			})
			defer patch1.Reset()

			patch2 := ApplyMethod(reflect.TypeOf(afsUtils), "FindUnmountedAlluxioPaths", func(_ operations.AlluxioFileUtils, alluxioPaths []string) ([]string, error) {
				return alluxioPaths, nil
			})
			defer patch2.Reset()

			e := &AlluxioEngine{
				runtime:            &runtime,
				name:               "test",
				namespace:          "default",
				Log:                fake.NullLogger(),
				Client:             mockClient,
				MetadataSyncDoneCh: nil,
			}

			unmountedPaths, err := e.FindUnmountedUFS()
			if err != nil {
				t.Errorf("AlluxioEngine.FindUnmountedUFS() error = %v", err)
				return
			}
			if (len(unmountedPaths) != 0 || len(test.wantedUnmountedPaths) != 0) &&
				!reflect.DeepEqual(unmountedPaths, test.wantedUnmountedPaths) {
				t.Errorf("%d check failure, want: %s, got: %s", index, strings.Join(test.wantedUnmountedPaths, ","), strings.Join(unmountedPaths, ","))
				return
			}
		})
	}
}

// TestUpdateMountTime verifies if AlluxioEngine's updateMountTime method correctly updates runtime's MountTime status.
// It creates a runtime with outdated MountTime, executes the update method, then checks if MountTime gets refreshed timestamp.
//
// param: t *testing.T - The testing context used for running the test and reporting failures.
//
// returns: None (This is a test function and does not return any value.)
func TestUpdateMountTime(t *testing.T) {
	yesterday := time.Now().AddDate(0, 0, -1)

	type fields struct {
		runtime *datav1alpha1.AlluxioRuntime
	}

	tests := []fields{
		{
			runtime: &datav1alpha1.AlluxioRuntime{
				ObjectMeta: v1.ObjectMeta{
					Name:      "test",
					Namespace: "default",
				},
				Status: datav1alpha1.RuntimeStatus{
					MountTime: &v1.Time{
						Time: yesterday,
					},
				},
			},
		},
	}

	for index, test := range tests {
		t.Run("test", func(t *testing.T) {
			s := runtime.NewScheme()
			s.AddKnownTypes(datav1alpha1.GroupVersion, test.runtime)
			_ = corev1.AddToScheme(s)
			mockClient := fake.NewFakeClientWithScheme(s, test.runtime)

			e := &AlluxioEngine{
				runtime:            test.runtime,
				name:               "test",
				namespace:          "default",
				Log:                fake.NullLogger(),
				Client:             mockClient,
				MetadataSyncDoneCh: nil,
			}

			e.updateMountTime()
			runtime, _ := e.getRuntime()
			if runtime.Status.MountTime.Time.Equal(yesterday) {
				t.Errorf("%d check failure, got: %v, unexpected: %v", index, runtime.Status.MountTime.Time, yesterday)
				return
			}
		})
	}
}

// TestCheckIfRemountRequired tests the checkIfRemountRequired function in AlluxioEngine.
// It verifies whether the system correctly identifies when a remount is required based on:
//   - Runtime's last mount time
//   - Pod's container start time
//   - Dataset mount configurations
//
// Test cases:
//  1. When pod started AFTER last mount time (expect remount)
//     - Runtime mount time: yesterday
//     - Pod start time: yesterday + 1 day
//     - Expected: ["/path"] (remount required)
//  2. When pod started BEFORE last mount time (expect no remount)
//     - Runtime mount time: yesterday
//     - Pod start time: yesterday - 1 day
//     - Expected: [] (no remount needed)
//
// The test:
//   - Creates mock runtime, pod and dataset objects
//   - Initializes fake Kubernetes client with test objects
//   - Mocks AlluxioFileUtils operations:
//   - Always reports Ready() = true
//   - FindUnmountedAlluxioPaths() returns original paths
//   - Compares actual remount paths with expected results
func TestCheckIfRemountRequired(t *testing.T) {
	yesterday := time.Now().AddDate(0, 0, -1)

	type fields struct {
		runtime *datav1alpha1.AlluxioRuntime
		pod     *corev1.Pod
		wanted  []string
	}

	tests := []fields{
		{
			runtime: &datav1alpha1.AlluxioRuntime{
				ObjectMeta: v1.ObjectMeta{
					Name:      "test",
					Namespace: "default",
				},
				Status: datav1alpha1.RuntimeStatus{
					MountTime: &v1.Time{
						Time: yesterday,
					},
				},
			},
			pod: &corev1.Pod{
				ObjectMeta: v1.ObjectMeta{
					Name:      "test-master-0",
					Namespace: "default",
				},
				Status: corev1.PodStatus{
					ContainerStatuses: []corev1.ContainerStatus{
						{
							Name: "alluxio-master",
							State: corev1.ContainerState{
								Running: &corev1.ContainerStateRunning{
									StartedAt: v1.Time{
										Time: yesterday.AddDate(0, 0, 1),
									},
								},
							},
						},
					},
				},
			},
			wanted: []string{
				"/path",
			},
		},
		{
			runtime: &datav1alpha1.AlluxioRuntime{
				ObjectMeta: v1.ObjectMeta{
					Name:      "test",
					Namespace: "default",
				},
				Status: datav1alpha1.RuntimeStatus{
					MountTime: &v1.Time{
						Time: yesterday,
					},
				},
			},
			pod: &corev1.Pod{
				ObjectMeta: v1.ObjectMeta{
					Name:      "test-master-0",
					Namespace: "default",
				},
				Status: corev1.PodStatus{
					ContainerStatuses: []corev1.ContainerStatus{
						{
							Name: "alluxio-master",
							State: corev1.ContainerState{
								Running: &corev1.ContainerStateRunning{
									StartedAt: v1.Time{
										Time: yesterday.AddDate(0, 0, -1),
									},
								},
							},
						},
					},
				},
			},
			wanted: []string{},
		},
	}

	dataset := datav1alpha1.Dataset{
		ObjectMeta: v1.ObjectMeta{
			Name:      "test",
			Namespace: "default",
		},
		Spec: datav1alpha1.DatasetSpec{
			Mounts: []datav1alpha1.Mount{
				{
					MountPoint: "s3://bucket/path/train",
					Path:       "/path",
				},
			},
		},
	}

	for index, test := range tests {
		t.Run("test", func(t *testing.T) {
			s := runtime.NewScheme()
			s.AddKnownTypes(datav1alpha1.GroupVersion, test.runtime)
			s.AddKnownTypes(datav1alpha1.GroupVersion, &dataset)
			s.AddKnownTypes(corev1.SchemeGroupVersion, test.pod)
			_ = corev1.AddToScheme(s)
			mockClient := fake.NewFakeClientWithScheme(s, test.runtime, &dataset, test.pod)

			e := &AlluxioEngine{
				runtime:            test.runtime,
				name:               "test",
				namespace:          "default",
				Log:                fake.NullLogger(),
				Client:             mockClient,
				MetadataSyncDoneCh: nil,
			}

			var afsUtils operations.AlluxioFileUtils
			patch1 := ApplyMethod(reflect.TypeOf(afsUtils), "Ready", func(_ operations.AlluxioFileUtils) bool {
				return true
			})
			defer patch1.Reset()

			patch2 := ApplyMethod(reflect.TypeOf(afsUtils), "FindUnmountedAlluxioPaths", func(_ operations.AlluxioFileUtils, alluxioPaths []string) ([]string, error) {
				return alluxioPaths, nil
			})
			defer patch2.Reset()

			ufsToUpdate := utils.NewUFSToUpdate(&dataset)
			e.checkIfRemountRequired(ufsToUpdate)
			if (len(ufsToUpdate.ToAdd()) != 0 || len(test.wanted) != 0) &&
				!reflect.DeepEqual(ufsToUpdate.ToAdd(), test.wanted) {
				t.Errorf("%d check failure, got: %v, expected: %s", index, ufsToUpdate.ToAdd(), test.wanted)
				return
			}
		})
	}
}
