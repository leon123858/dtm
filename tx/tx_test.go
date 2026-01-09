package tx

import (
	"fmt"
	"math"
	"reflect"
	"sort"
	"strings"
	"testing"
)

func TestTxPackage_ProcessTransactions(t *testing.T) {
	tests := []struct {
		name             string
		txPackage        Package
		expectedCashList []Cash
	}{
		{
			name: "Single transaction, two addresses",
			txPackage: Package{
				Name: "SingleTxPackage",
				TxList: []Tx{
					{
						Input:  []Payment{{Amount: 10.0, Address: "Alice"}},
						Output: Payment{Amount: 10.0, Address: "Bob"},
						Name:   "Tx1",
					},
				},
			},
			expectedCashList: []Cash{
				{Address: "Alice", InputAmount: 10.0, OutputAmount: 0.0},
				{Address: "Bob", InputAmount: 0.0, OutputAmount: 10.0},
			},
		},
		{
			name: "Multiple transactions, addresses interacting",
			txPackage: Package{
				Name: "ComplexTxPackage",
				TxList: []Tx{
					{
						Input:  []Payment{{Amount: 100.0, Address: "Alice"}},
						Output: Payment{Amount: 100.0, Address: "Bob"},
						Name:   "TxA",
					},
					{
						Input:  []Payment{{Amount: 50.0, Address: "Bob"}}, // Bob sends money now
						Output: Payment{Amount: 50.0, Address: "Charlie"},
						Name:   "TxB",
					},
					{
						Input:  []Payment{{Amount: 20.0, Address: "Alice"}}, // Alice sends more
						Output: Payment{Amount: 20.0, Address: "David"},
						Name:   "TxC",
					},
				},
			},
			expectedCashList: []Cash{
				{Address: "Alice", InputAmount: 120.0, OutputAmount: 0.0},  // 100 from TxA, 20 from TxC
				{Address: "Bob", InputAmount: 50.0, OutputAmount: 100.0},   // 100 to Bob (TxA), 50 from Bob (TxB)
				{Address: "Charlie", InputAmount: 0.0, OutputAmount: 50.0}, // 50 to Charlie (TxB)
				{Address: "David", InputAmount: 0.0, OutputAmount: 20.0},   // 20 to David (TxC)
			},
		},
		{
			name: "Empty transaction list",
			txPackage: Package{
				Name:   "EmptyPackage",
				TxList: []Tx{},
			},
			expectedCashList: []Cash{}, // Expect an empty list
		},
		{
			name: "Transactions with zero amounts",
			txPackage: Package{
				Name: "ZeroAmountPackage",
				TxList: []Tx{
					{
						Input:  []Payment{{Amount: 0.0, Address: "X"}},
						Output: Payment{Amount: 0.0, Address: "Y"},
						Name:   "ZeroTx",
					},
				},
			},
			expectedCashList: []Cash{
				{Address: "X", InputAmount: 0.0, OutputAmount: 0.0},
				{Address: "Y", InputAmount: 0.0, OutputAmount: 0.0},
			},
		},
		{
			name: "Address appears in both input and output within the same package",
			txPackage: Package{
				Name: "SelfTransferPackage",
				TxList: []Tx{
					{
						Input:  []Payment{{Amount: 10.0, Address: "Alice"}},
						Output: Payment{Amount: 10.0, Address: "Bob"},
						Name:   "Tx1",
					},
					{
						Input:  []Payment{{Amount: 5.0, Address: "Bob"}},
						Output: Payment{Amount: 5.0, Address: "Alice"},
						Name:   "Tx2",
					},
				},
			},
			expectedCashList: []Cash{
				{Address: "Alice", InputAmount: 10.0, OutputAmount: 5.0}, // Input: 10 from Tx1. Output: 5 from Tx2
				{Address: "Bob", InputAmount: 5.0, OutputAmount: 10.0},   // Input: 5 from Tx2. Output: 10 from Tx1
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotCashList := tt.txPackage.ProcessTransactions()

			// Sort both slices to ensure consistent order for comparison
			sort.Slice(gotCashList, func(i, j int) bool {
				return gotCashList[i].Address < gotCashList[j].Address
			})
			sort.Slice(tt.expectedCashList, func(i, j int) bool {
				return tt.expectedCashList[i].Address < tt.expectedCashList[j].Address
			})

			// Check if lengths match
			if len(gotCashList) != len(tt.expectedCashList) {
				t.Errorf("ProcessTransactions() gotCashList length = %v, want %v", len(gotCashList), len(tt.expectedCashList))
				return
			}

			// Compare each Cash struct
			for i := range gotCashList {
				if !reflect.DeepEqual(gotCashList[i], tt.expectedCashList[i]) {
					t.Errorf("ProcessTransactions() at index %d: got %v, want %v", i, gotCashList[i], tt.expectedCashList[i])
				}
			}
		})
	}
}

func TestPackage_SetNoSmallValue_EdgeCases(t *testing.T) {
	tests := []struct {
		name        string
		minValue    float64
		initialPkg  Package
		expectedPkg Package
	}{
		{
			name:     "剛好等於門檻值 (不應被移除)",
			minValue: 1.0,
			initialPkg: Package{
				TxList: []Tx{
					{
						Input:  []Payment{{Address: "A", Amount: 1.0}}, // 1.0 < 1.0 為假
						Output: Payment{Address: "B", Amount: 1.0},
					},
				},
			},
			expectedPkg: Package{
				TxList: []Tx{
					{
						Input:  []Payment{{Address: "A", Amount: 1.0}},
						Output: Payment{Address: "B", Amount: 1.0},
					},
				},
			},
		},
		{
			name:     "微小浮點數差距 (略小於門檻)",
			minValue: 1.0,
			initialPkg: Package{
				TxList: []Tx{
					{
						Input:  []Payment{{Address: "A", Amount: 0.999999999}},
						Output: Payment{Address: "B", Amount: 1.0},
					},
				},
			},
			expectedPkg: Package{
				TxList: []Tx{
					{
						Input:  []Payment{{Address: "A", Amount: 0.0}},
						Output: Payment{Address: "B", Amount: 0.0}, // 1.0 - 0.99... = 0.000... < 1.0 故歸零
					},
				},
			},
		},
		{
			name:     "空 Package 與空交易清單",
			minValue: 1.0,
			initialPkg: Package{
				Name:   "Empty",
				TxList: []Tx{},
			},
			expectedPkg: Package{
				Name:   "Empty",
				TxList: []Tx{},
			},
		},
		{
			name:     "交易中沒有任何 Input",
			minValue: 1.0,
			initialPkg: Package{
				TxList: []Tx{
					{
						Name:   "NoInput",
						Input:  []Payment{},
						Output: Payment{Address: "B", Amount: 0.5}, // 直接檢查 Output
					},
				},
			},
			expectedPkg: Package{
				TxList: []Tx{
					{
						Name:   "NoInput",
						Input:  []Payment{},
						Output: Payment{Address: "B", Amount: 0.0}, // 0.5 < 1.0
					},
				},
			},
		},
		{
			name:     "極端情況：所有 Input 移除後 Output 剛好等於門檻值",
			minValue: 10.0,
			initialPkg: Package{
				TxList: []Tx{
					{
						Input: []Payment{
							{Address: "A", Amount: 5.0},  // 移除
							{Address: "B", Amount: 15.0}, // 保留
						},
						Output: Payment{Address: "C", Amount: 20.0},
					},
				},
			},
			expectedPkg: Package{
				TxList: []Tx{
					{
						Input: []Payment{
							{Address: "A", Amount: 0.0},
							{Address: "B", Amount: 15.0},
						},
						Output: Payment{Address: "C", Amount: 15.0}, // 20 - 5 = 15 (>= 10, 保留)
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.initialPkg.SetNoSmallValue(tt.minValue)

			if !reflect.DeepEqual(tt.initialPkg, tt.expectedPkg) {
				t.Errorf("%s 測試失敗:\nGot:  %+v\nWant: %+v", tt.name, tt.initialPkg, tt.expectedPkg)
			}
		})
	}
}

func TestPackage_SetNoSmallValue_WithPanics(t *testing.T) {
	tests := []struct {
		name        string
		minValue    float64
		initialPkg  Package
		expectedPkg Package
		shouldPanic bool
		panicMsg    string
	}{
		{
			name:        "minValue 為零 (不應移除任何正數金額)",
			minValue:    0.0,
			shouldPanic: true,
			initialPkg: Package{
				TxList: []Tx{
					{
						Input:  []Payment{{Address: "A", Amount: 0.00001}},
						Output: Payment{Address: "B", Amount: 0.00001},
					},
				},
			},
			expectedPkg: Package{
				TxList: []Tx{
					{
						Input:  []Payment{{Address: "A", Amount: 0.00001}},
						Output: Payment{Address: "B", Amount: 0.00001},
					},
				},
			},
		},
		{
			name:        "正常情境：移除微小金額並調整輸出",
			minValue:    1.0,
			shouldPanic: false,
			initialPkg: Package{
				TxList: []Tx{
					{
						Input: []Payment{
							{Address: "A", Amount: 10.0},
							{Address: "B", Amount: 0.5}, // 應被歸零
						},
						Output: Payment{Address: "C", Amount: 10.5},
					},
				},
			},
			expectedPkg: Package{
				TxList: []Tx{
					{
						Input: []Payment{
							{Address: "A", Amount: 10.0},
							{Address: "B", Amount: 0.0},
						},
						Output: Payment{Address: "C", Amount: 10.0},
					},
				},
			},
		},
		{
			name:        "Panic：minValue 低於 epsilon",
			minValue:    epsilon / 2, // 假設 epsilon 為 1e-9
			shouldPanic: true,
			panicMsg:    "minValue < epsilon",
			initialPkg:  Package{TxList: []Tx{{}}},
		},
		{
			name:        "Panic：輸入金額為負數",
			minValue:    1.0,
			shouldPanic: true,
			panicMsg:    "Input amount should not negative",
			initialPkg: Package{
				TxList: []Tx{
					{
						Input:  []Payment{{Address: "A", Amount: -0.5}},
						Output: Payment{Address: "B", Amount: 10.0},
					},
				},
			},
		},
		{
			name:        "Panic：扣除後輸出變為負數",
			minValue:    1.0,
			shouldPanic: true,
			panicMsg:    "Output amount should not negative",
			initialPkg: Package{
				TxList: []Tx{
					{
						Input:  []Payment{{Address: "A", Amount: 0.5}},
						Output: Payment{Address: "B", Amount: 0.2}, // 0.2 - 0.5 = -0.3
					},
				},
			},
		},
		{
			name:        "邊界：輸出扣除後剛好低於門檻 (應歸零)",
			minValue:    1.0,
			shouldPanic: false,
			initialPkg: Package{
				TxList: []Tx{
					{
						Input:  []Payment{{Address: "A", Amount: 0.5}},
						Output: Payment{Address: "B", Amount: 1.2}, // 1.2 - 0.5 = 0.7 (< 1.0)
					},
				},
			},
			expectedPkg: Package{
				TxList: []Tx{
					{
						Input:  []Payment{{Address: "A", Amount: 0.0}},
						Output: Payment{Address: "B", Amount: 0.0},
					},
				},
			},
		},
	}

	contains := func(s, substr string) bool {
		return strings.Contains(s, substr)
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// 處理 Panic 驗證
			defer func() {
				r := recover()
				if tt.shouldPanic {
					if r == nil {
						t.Errorf("預期發生 panic 但程式未發生")
					} else if !contains(fmt.Sprint(r), tt.panicMsg) {
						t.Errorf("Panic 訊息不符合。Got: %v, Want to contain: %v", r, tt.panicMsg)
					}
				} else if r != nil {
					t.Errorf("不預期發生 panic 但程式發生了: %v", r)
				}
			}()

			// 執行目標函式
			tt.initialPkg.SetNoSmallValue(tt.minValue)

			// 若不應發生 panic，檢查結果
			if !tt.shouldPanic {
				if !reflect.DeepEqual(tt.initialPkg, tt.expectedPkg) {
					t.Errorf("結果不符合預期。\nGot: %+v\nWant: %+v", tt.initialPkg, tt.expectedPkg)
				}
			}
		})
	}
}

func TestPackage_DropZeroTx(t *testing.T) {
	tests := []struct {
		name     string
		initial  Package
		expected Package
	}{
		{
			name: "移除 Output 為 0 的交易",
			initial: Package{
				TxList: []Tx{
					{
						Name:   "ValidTx",
						Input:  []Payment{{Address: "A", Amount: 10.0}},
						Output: Payment{Address: "B", Amount: 10.0},
					},
					{
						Name:   "ZeroOutputTx",
						Input:  []Payment{{Address: "C", Amount: 10.0}},
						Output: Payment{Address: "D", Amount: 0.0},
					},
				},
			},
			expected: Package{
				TxList: []Tx{
					{
						Name:   "ValidTx",
						Input:  []Payment{{Address: "A", Amount: 10.0}},
						Output: Payment{Address: "B", Amount: 10.0},
					},
				},
			},
		},
		{
			name: "保留交易但移除為 0 的 Input",
			initial: Package{
				TxList: []Tx{
					{
						Name: "MixedInputTx",
						Input: []Payment{
							{Address: "A", Amount: 10.0},
							{Address: "B", Amount: 0.0},
							{Address: "C", Amount: 0.00000000001}, // 低於 epsilon
						},
						Output: Payment{Address: "D", Amount: 10.0},
					},
				},
			},
			expected: Package{
				TxList: []Tx{
					{
						Name:   "MixedInputTx",
						Input:  []Payment{{Address: "A", Amount: 10.0}},
						Output: Payment{Address: "D", Amount: 10.0},
					},
				},
			},
		},
		{
			name: "所有交易皆為 0 時回傳空清單",
			initial: Package{
				TxList: []Tx{
					{Output: Payment{Amount: 0.0}},
					{Output: Payment{Amount: epsilon / 2}},
				},
			},
			expected: Package{
				TxList: []Tx{},
			},
		},
		{
			name: "空封裝處理",
			initial: Package{
				Name:   "Empty",
				TxList: []Tx{},
			},
			expected: Package{
				Name:   "Empty",
				TxList: []Tx{},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.initial.DropZeroTx()

			if len(tt.initial.TxList) != len(tt.expected.TxList) {
				t.Fatalf("TxList 長度不符。Got: %d, Want: %d", len(tt.initial.TxList), len(tt.expected.TxList))
			}

			if !reflect.DeepEqual(tt.initial, tt.expected) {
				t.Errorf("結果不符合預期。\nGot: %+v\nWant: %+v", tt.initial, tt.expected)
			}
		})
	}
}

func TestShareMoneyEasy_Integration_Complex(t *testing.T) {
	tests := []struct {
		name          string
		uiList        []UserPayment
		minValue      float64
		expectedCount int     // 預期最終 Tx 數量
		expectedDiff  float64 // 預期剩餘金額誤差
		expectErr     bool
	}{
		{
			name: "場景 1：循環債務自動抵銷 (A->B, B->C, C->A)",
			uiList: []UserPayment{
				{Name: "T1", Amount: 100, PrePayAddress: "A", ShouldPayAddress: []string{"B"}, ExtendPayMsg: []float64{0.0}, PaymentType: 0}, // FixMoney: B 欠 A 100
				{Name: "T2", Amount: 100, PrePayAddress: "B", ShouldPayAddress: []string{"C"}, ExtendPayMsg: []float64{0.0}, PaymentType: 0}, // FixMoney: C 欠 B 100
				{Name: "T3", Amount: 100, PrePayAddress: "C", ShouldPayAddress: []string{"A"}, ExtendPayMsg: []float64{0.0}, PaymentType: 0}, // FixMoney: A 欠 C 100
			},
			expectedCount: 0, // 應該全部抵銷，DropZeroTx 後不留交易
			expectedDiff:  0.0,
			expectErr:     false,
		},
		{
			name: "場景 3：自我支付與 Normalize (A 付 100 給 A 自己)",
			uiList: []UserPayment{
				{Name: "Self", Amount: 100, PrePayAddress: "A", ShouldPayAddress: []string{"A"}, PaymentType: 0},
			},
			expectedCount: 0, // NormalizeCash 會將 A 的 Input/Output 抵銷為 0
			expectedDiff:  0.0,
			expectErr:     false,
		},
		{
			name: "場景 4：混合多種 Strategy 的複雜分帳",
			uiList: []UserPayment{
				{
					Name: "Lunch", Amount: 300, PrePayAddress: "A",
					ShouldPayAddress: []string{"B", "C"}, PaymentType: 0, // B:150, C:150
				},
				{
					Name: "Taxi", Amount: 100, PrePayAddress: "B",
					ShouldPayAddress: []string{"A"}, PaymentType: 1, ExtendPayMsg: []float64{100}, // A 欠 B 100
				},
			},
			expectedCount: 1,
			expectedDiff:  0.0,
			expectErr:     false,
		},
		{
			name: "場景 5：金額剛好等於 Epsilon (邊緣值處理)",
			uiList: []UserPayment{
				{Name: "EpsilonTx", Amount: epsilon, PrePayAddress: "A", ShouldPayAddress: []string{"B"}, PaymentType: 0},
			},
			expectedCount: 0, // epsilon <= epsilon，會被 DropZeroTx 濾掉
			expectedDiff:  0.0,
			expectErr:     false,
		},
		{
			name: "場景 6：無效 UserPayment (金額為 0) 應觸發錯誤",
			uiList: []UserPayment{
				{Name: "Invalid", Amount: 0, PrePayAddress: "A", ShouldPayAddress: []string{"B"}, PaymentType: 0},
			},
			expectedCount: 0,
			expectErr:     true, // UIList2TxList 會回傳錯誤
		},
		{
			name: "場景 7：巨額分帳下的浮點數累加誤差",
			uiList: func() []UserPayment {
				var list []UserPayment
				for i := 0; i < 1000; i++ {
					list = append(list, UserPayment{
						Name: "Heavy", Amount: 0.01, PrePayAddress: "A", ShouldPayAddress: []string{"B"}, PaymentType: 0,
					})
				}
				return list
			}(), // A 總共付了 10.0 給 B
			expectedCount: 1, // 最終應合併為一筆約 10.0 的交易
			expectedDiff:  0.0,
			expectErr:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pkg, diff, err := ShareMoneyEasy(tt.uiList)

			if (err != nil) != tt.expectErr {
				t.Fatalf("Error 狀態不符: got %v, wantErr %v", err, tt.expectErr)
			}

			if !tt.expectErr {
				if len(pkg.TxList) != tt.expectedCount {
					t.Errorf("交易數量不符: got %d, want %d", len(pkg.TxList), tt.expectedCount)
				}

				if math.Abs(diff-tt.expectedDiff) > epsilon {
					t.Errorf("Diff 金額不符: got %f, want %f", diff, tt.expectedDiff)
				}

				// 驗證 DropZeroTx 是否徹底
				for _, tx := range pkg.TxList {
					if tx.Output.Amount <= tt.minValue && tx.Output.Amount != 0 {
						t.Errorf("發現未清理的微小 Output: %f (minValue: %f)", tx.Output.Amount, tt.minValue)
					}
				}
			}
		})
	}
}

func TestTx_Validate(t *testing.T) {
	tests := []struct {
		name           string
		tx             Tx
		expectedInput  float64
		expectedOutput float64
	}{
		{
			name: "Single input and output",
			tx: Tx{
				Input:  []Payment{{Amount: 10.0, Address: "Alice"}},
				Output: Payment{Amount: 10.0, Address: "Bob"},
				Name:   "Tx1",
			},
			expectedInput:  10.0,
			expectedOutput: 10.0,
		},
		{
			name: "Multiple inputs, single output",
			tx: Tx{
				Input: []Payment{
					{Amount: 5.0, Address: "Alice"},
					{Amount: 15.0, Address: "Bob"},
				},
				Output: Payment{Amount: 20.0, Address: "Charlie"},
				Name:   "Tx2",
			},
			expectedInput:  20.0,
			expectedOutput: 20.0,
		},
		{
			name: "No inputs, only output",
			tx: Tx{
				Input:  []Payment{},
				Output: Payment{Amount: 10.0, Address: "Charlie"},
				Name:   "Tx3",
			},
			expectedInput:  0.0,
			expectedOutput: 10.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input, output := tt.tx.Validate()
			if input != tt.expectedInput || output != tt.expectedOutput {
				t.Errorf("Validate() = (%v, %v), want (%v, %v)", input, output, tt.expectedInput, tt.expectedOutput)
			}
		})
	}
}

func TestTxPackage_String(t *testing.T) {
	tests := []struct {
		name      string
		txPackage Package
		expected  string
	}{
		{
			name: "Single transaction package",
			txPackage: Package{
				Name: "TestPackage",
				TxList: []Tx{
					{
						Input:  []Payment{{Amount: 10.0, Address: "Alice"}},
						Output: Payment{Amount: 10.0, Address: "Bob"},
						Name:   "Tx1",
					},
				},
			},
		},
		{
			name: "Empty transaction package",
			txPackage: Package{
				Name:   "EmptyPackage",
				TxList: []Tx{},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.txPackage.String(); got == "" {
				t.Errorf("TxPackage.String() returned an empty string, expected non-empty string")
			} else {
				println("TxPackage.String() output:", got)
			}
		})
	}
}
