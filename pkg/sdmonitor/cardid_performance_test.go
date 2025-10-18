package sdmonitor

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

// BenchmarkGenerateCardID benchmarks card ID generation
func BenchmarkGenerateCardID(b *testing.B) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		id := generateCardID()
		if len(id) == 0 {
			b.Fatal("generated empty card ID")
		}
	}
}

// BenchmarkGenerateCardIDParallel benchmarks parallel card ID generation
func BenchmarkGenerateCardIDParallel(b *testing.B) {
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			id := generateCardID()
			if len(id) == 0 {
				b.Fatal("generated empty card ID")
			}
		}
	})
}

// BenchmarkGenerateCardIDUniqueness tests generation speed and uniqueness
func BenchmarkGenerateCardIDUniqueness(b *testing.B) {
	seen := make(map[string]bool)
	var mu sync.Mutex

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		id := generateCardID()
		mu.Lock()
		if seen[id] {
			b.Fatalf("duplicate card ID generated: %s", id)
		}
		seen[id] = true
		mu.Unlock()
	}

	b.Logf("Generated %d unique IDs", len(seen))
}

// BenchmarkGetOrCreateCardID_ExistingID benchmarks reading existing card ID
func BenchmarkGetOrCreateCardID_ExistingID(b *testing.B) {
	tmpDir := b.TempDir()

	// Create existing card ID file
	idPath := filepath.Join(tmpDir, CardIDFile)
	testID := "card-test123456"
	if err := os.WriteFile(idPath, []byte(testID+"\n"), 0644); err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		id, isNew, err := GetOrCreateCardID(tmpDir, nil)
		if err != nil {
			b.Fatal(err)
		}
		if isNew {
			b.Fatal("expected existing card, got new")
		}
		if id != testID {
			b.Fatalf("expected ID %s, got %s", testID, id)
		}
	}
}

// BenchmarkGetOrCreateCardID_NewID benchmarks creating new card ID
func BenchmarkGetOrCreateCardID_NewID(b *testing.B) {
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		tmpDir := b.TempDir()
		b.StartTimer()

		id, isNew, err := GetOrCreateCardID(tmpDir, nil)
		if err != nil {
			b.Fatal(err)
		}
		if !isNew {
			b.Fatal("expected new card, got existing")
		}
		if len(id) == 0 {
			b.Fatal("generated empty card ID")
		}
	}
}

// BenchmarkGetOrCreateCardID_ExistingIDMemory measures memory allocations
func BenchmarkGetOrCreateCardID_ExistingIDMemory(b *testing.B) {
	tmpDir := b.TempDir()

	// Create existing card ID file
	idPath := filepath.Join(tmpDir, CardIDFile)
	testID := "card-test123456"
	if err := os.WriteFile(idPath, []byte(testID+"\n"), 0644); err != nil {
		b.Fatal(err)
	}

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_, _, err := GetOrCreateCardID(tmpDir, nil)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkCreateNewCardID benchmarks forced creation of new card ID
func BenchmarkCreateNewCardID(b *testing.B) {
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		tmpDir := b.TempDir()
		b.StartTimer()

		id, err := CreateNewCardID(tmpDir, nil)
		if err != nil {
			b.Fatal(err)
		}
		if len(id) == 0 {
			b.Fatal("generated empty card ID")
		}
	}
}

// BenchmarkCreateNewCardID_OverwriteExisting benchmarks overwriting existing ID
func BenchmarkCreateNewCardID_OverwriteExisting(b *testing.B) {
	tmpDir := b.TempDir()

	// Create initial card ID file
	idPath := filepath.Join(tmpDir, CardIDFile)
	oldID := "card-old123456"
	if err := os.WriteFile(idPath, []byte(oldID+"\n"), 0644); err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		id, err := CreateNewCardID(tmpDir, nil)
		if err != nil {
			b.Fatal(err)
		}
		if id == oldID {
			b.Fatal("expected new ID, got old ID")
		}
	}
}

// BenchmarkGetOrCreateCardIDParallel benchmarks concurrent card ID operations
func BenchmarkGetOrCreateCardIDParallel(b *testing.B) {
	tmpDir := b.TempDir()

	// Create existing card ID file
	idPath := filepath.Join(tmpDir, CardIDFile)
	testID := "card-test123456"
	if err := os.WriteFile(idPath, []byte(testID+"\n"), 0644); err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			id, _, err := GetOrCreateCardID(tmpDir, nil)
			if err != nil {
				b.Fatal(err)
			}
			if id != testID {
				b.Fatalf("expected ID %s, got %s", testID, id)
			}
		}
	})
}

// BenchmarkCardIDFileOperations benchmarks raw file I/O for card ID
func BenchmarkCardIDFileOperations(b *testing.B) {
	testCases := []struct {
		name string
		op   func(string) error
	}{
		{
			name: "read",
			op: func(path string) error {
				_, err := os.ReadFile(filepath.Join(path, CardIDFile))
				return err
			},
		},
		{
			name: "write",
			op: func(path string) error {
				return os.WriteFile(filepath.Join(path, CardIDFile), []byte("card-test\n"), 0644)
			},
		},
		{
			name: "read_write",
			op: func(path string) error {
				if _, err := os.ReadFile(filepath.Join(path, CardIDFile)); err == nil {
					return nil
				}
				return os.WriteFile(filepath.Join(path, CardIDFile), []byte("card-test\n"), 0644)
			},
		},
	}

	for _, tc := range testCases {
		b.Run(tc.name, func(b *testing.B) {
			tmpDir := b.TempDir()
			// Pre-create file for read tests
			if tc.name == "read" || tc.name == "read_write" {
				if err := os.WriteFile(filepath.Join(tmpDir, CardIDFile), []byte("card-test\n"), 0644); err != nil {
					b.Fatal(err)
				}
			}

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if err := tc.op(tmpDir); err != nil && tc.name != "read_write" {
					b.Fatal(err)
				}
			}
		})
	}
}

// BenchmarkCardIDStressTest stress tests card ID operations under load
func BenchmarkCardIDStressTest(b *testing.B) {
	tmpDir := b.TempDir()
	const concurrency = 100

	// Create initial card ID
	testID := "card-stress123"
	if err := os.WriteFile(filepath.Join(tmpDir, CardIDFile), []byte(testID+"\n"), 0644); err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()

	var wg sync.WaitGroup
	errors := make(chan error, concurrency*b.N)

	for i := 0; i < b.N; i++ {
		for j := 0; j < concurrency; j++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_, _, err := GetOrCreateCardID(tmpDir, nil)
				if err != nil {
					errors <- err
				}
			}()
		}
	}

	wg.Wait()
	close(errors)

	if len(errors) > 0 {
		b.Fatalf("got %d errors during stress test: %v", len(errors), <-errors)
	}
}

// BenchmarkCardIDCollisionTest tests for ID collisions at scale
func BenchmarkCardIDCollisionTest(b *testing.B) {
	const iterations = 10000

	seen := make(map[string]bool)
	collisions := 0

	b.ResetTimer()

	for i := 0; i < iterations; i++ {
		id := generateCardID()
		if seen[id] {
			collisions++
		}
		seen[id] = true
	}

	if collisions > 0 {
		b.Fatalf("found %d collisions out of %d IDs", collisions, iterations)
	}

	b.Logf("Generated %d unique IDs with 0 collisions", len(seen))
}

// BenchmarkCardIDWithLargeFiles benchmarks ID operations with large card ID files
func BenchmarkCardIDWithLargeFiles(b *testing.B) {
	tmpDir := b.TempDir()

	// Create card ID file with extra whitespace/newlines
	idPath := filepath.Join(tmpDir, CardIDFile)
	testID := "card-test123456"
	// Add lots of whitespace to simulate corruption or manual editing
	content := fmt.Sprintf("\n\n\n%s\n\n\n", testID)
	if err := os.WriteFile(idPath, []byte(content), 0644); err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		id, _, err := GetOrCreateCardID(tmpDir, nil)
		if err != nil {
			b.Fatal(err)
		}
		if id != testID {
			b.Fatalf("expected ID %s, got %s", testID, id)
		}
	}
}
