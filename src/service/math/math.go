package math

import (
	"fmt"
	"math"
	"math/big"
	"math/rand"
	"strconv"
	"strings"
)

// Service provides mathematical utilities
type Service struct{}

// New creates a new Math service
func New() *Service {
	return &Service{}
}

// Errors
var (
	ErrDivisionByZero     = fmt.Errorf("division by zero")
	ErrNegativeSquareRoot = fmt.Errorf("cannot take square root of negative number")
)

// Basic operations
func (s *Service) Add(a, b float64) float64 {
	return a + b
}

func (s *Service) Subtract(a, b float64) float64 {
	return a - b
}

func (s *Service) Multiply(a, b float64) float64 {
	return a * b
}

func (s *Service) Divide(a, b float64) (float64, error) {
	if b == 0 {
		return 0, ErrDivisionByZero
	}
	return a / b, nil
}

func (s *Service) Modulo(a, b int64) (int64, error) {
	if b == 0 {
		return 0, ErrDivisionByZero
	}
	return a % b, nil
}

func (s *Service) Power(base, exponent float64) float64 {
	return math.Pow(base, exponent)
}

func (s *Service) SquareRoot(n float64) (float64, error) {
	if n < 0 {
		return 0, ErrNegativeSquareRoot
	}
	return math.Sqrt(n), nil
}

func (s *Service) CubeRoot(n float64) float64 {
	return math.Cbrt(n)
}

// Trigonometric functions
func (s *Service) Sin(angle float64) float64 {
	return math.Sin(angle)
}

func (s *Service) Cos(angle float64) float64 {
	return math.Cos(angle)
}

func (s *Service) Tan(angle float64) float64 {
	return math.Tan(angle)
}

func (s *Service) Asin(x float64) float64 {
	return math.Asin(x)
}

func (s *Service) Acos(x float64) float64 {
	return math.Acos(x)
}

func (s *Service) Atan(x float64) float64 {
	return math.Atan(x)
}

// Angle conversions
func (s *Service) DegreesToRadians(degrees float64) float64 {
	return degrees * math.Pi / 180
}

func (s *Service) RadiansToDegrees(radians float64) float64 {
	return radians * 180 / math.Pi
}

// Logarithmic functions
func (s *Service) Log(x float64) float64 {
	return math.Log(x)
}

func (s *Service) Log10(x float64) float64 {
	return math.Log10(x)
}

func (s *Service) Log2(x float64) float64 {
	return math.Log2(x)
}

func (s *Service) Exp(x float64) float64 {
	return math.Exp(x)
}

// Rounding functions
func (s *Service) Round(x float64) float64 {
	return math.Round(x)
}

func (s *Service) Floor(x float64) float64 {
	return math.Floor(x)
}

func (s *Service) Ceil(x float64) float64 {
	return math.Ceil(x)
}

func (s *Service) Abs(x float64) float64 {
	return math.Abs(x)
}

// Statistical functions
func (s *Service) Min(numbers []float64) float64 {
	if len(numbers) == 0 {
		return 0
	}
	min := numbers[0]
	for _, n := range numbers[1:] {
		if n < min {
			min = n
		}
	}
	return min
}

func (s *Service) Max(numbers []float64) float64 {
	if len(numbers) == 0 {
		return 0
	}
	max := numbers[0]
	for _, n := range numbers[1:] {
		if n > max {
			max = n
		}
	}
	return max
}

func (s *Service) Sum(numbers []float64) float64 {
	sum := 0.0
	for _, n := range numbers {
		sum += n
	}
	return sum
}

func (s *Service) Average(numbers []float64) float64 {
	if len(numbers) == 0 {
		return 0
	}
	return s.Sum(numbers) / float64(len(numbers))
}

func (s *Service) Median(numbers []float64) float64 {
	if len(numbers) == 0 {
		return 0
	}
	sorted := make([]float64, len(numbers))
	copy(sorted, numbers)
	// Simple bubble sort for median
	for i := 0; i < len(sorted); i++ {
		for j := i + 1; j < len(sorted); j++ {
			if sorted[i] > sorted[j] {
				sorted[i], sorted[j] = sorted[j], sorted[i]
			}
		}
	}
	mid := len(sorted) / 2
	if len(sorted)%2 == 0 {
		return (sorted[mid-1] + sorted[mid]) / 2
	}
	return sorted[mid]
}

// Number theory
func (s *Service) Factorial(n int64) *big.Int {
	result := big.NewInt(1)
	for i := int64(2); i <= n; i++ {
		result.Mul(result, big.NewInt(i))
	}
	return result
}

func (s *Service) IsPrime(n int64) bool {
	if n < 2 {
		return false
	}
	if n == 2 {
		return true
	}
	if n%2 == 0 {
		return false
	}
	sqrt := int64(math.Sqrt(float64(n)))
	for i := int64(3); i <= sqrt; i += 2 {
		if n%i == 0 {
			return false
		}
	}
	return true
}

func (s *Service) GCD(a, b int64) int64 {
	for b != 0 {
		a, b = b, a%b
	}
	return a
}

func (s *Service) LCM(a, b int64) int64 {
	// LCM(0, x) is defined as 0; guarding also avoids a divide-by-zero
	// panic since GCD(0, 0) is 0.
	if a == 0 || b == 0 {
		return 0
	}
	return (a * b) / s.GCD(a, b)
}

// Random numbers
func (s *Service) RandomInt(min, max int64) int64 {
	// Normalize an inverted range so callers can pass bounds in any order
	// without triggering a panic from a non-positive argument.
	if max < min {
		min, max = max, min
	}
	if min == max {
		return min
	}
	// span is max-min+1; when that would overflow int64 (an effectively
	// full-width range), fall back to an unbounded 63-bit draw rather than
	// panicking on the overflowed argument.
	span := max - min
	if span == math.MaxInt64 {
		return min + rand.Int63()
	}
	return min + rand.Int63n(span+1)
}

func (s *Service) RandomFloat(min, max float64) float64 {
	return min + rand.Float64()*(max-min)
}

// Percentage calculations
func (s *Service) PercentageOf(part, whole float64) float64 {
	if whole == 0 {
		return 0
	}
	return (part / whole) * 100
}

func (s *Service) PercentageChange(oldVal, newVal float64) float64 {
	if oldVal == 0 {
		return 0
	}
	return ((newVal - oldVal) / oldVal) * 100
}

// Fibonacci returns the first count numbers of the Fibonacci sequence
// (0, 1, 1, 2, 3, 5, ...) as *big.Int for overflow safety on large counts
func (s *Service) Fibonacci(count int) ([]*big.Int, error) {
	if count < 0 {
		return nil, fmt.Errorf("count must be non-negative")
	}
	result := make([]*big.Int, count)
	a, b := big.NewInt(0), big.NewInt(1)
	for i := 0; i < count; i++ {
		result[i] = new(big.Int).Set(a)
		a, b = b, new(big.Int).Add(a, b)
	}
	return result, nil
}

// BaseConvert converts number (expressed in fromBase) to its representation
// in toBase; both bases must be in the range 2-36
func (s *Service) BaseConvert(number string, fromBase, toBase int) (string, error) {
	if fromBase < 2 || fromBase > 36 {
		return "", fmt.Errorf("fromBase must be between 2 and 36")
	}
	if toBase < 2 || toBase > 36 {
		return "", fmt.Errorf("toBase must be between 2 and 36")
	}
	value, err := strconv.ParseInt(strings.TrimSpace(number), fromBase, 64)
	if err != nil {
		return "", fmt.Errorf("invalid number %q for base %d", number, fromBase)
	}
	return strconv.FormatInt(value, toBase), nil
}

// MatrixAdd adds two matrices of identical dimensions element-wise
func (s *Service) MatrixAdd(a, b [][]float64) ([][]float64, error) {
	if len(a) == 0 || len(b) == 0 || len(a) != len(b) {
		return nil, fmt.Errorf("matrices must have the same non-zero dimensions")
	}
	result := make([][]float64, len(a))
	for i := range a {
		if len(a[i]) != len(b[i]) {
			return nil, fmt.Errorf("matrices must have the same non-zero dimensions")
		}
		result[i] = make([]float64, len(a[i]))
		for j := range a[i] {
			result[i][j] = a[i][j] + b[i][j]
		}
	}
	return result, nil
}

// MatrixMultiply multiplies matrix a (m x n) by matrix b (n x p), returning
// the resulting m x p matrix
func (s *Service) MatrixMultiply(a, b [][]float64) ([][]float64, error) {
	if len(a) == 0 || len(b) == 0 {
		return nil, fmt.Errorf("matrices must be non-empty")
	}
	rowsA, colsA := len(a), len(a[0])
	rowsB, colsB := len(b), len(b[0])
	if colsA != rowsB {
		return nil, fmt.Errorf("matrix a columns (%d) must equal matrix b rows (%d)", colsA, rowsB)
	}
	result := make([][]float64, rowsA)
	for i := 0; i < rowsA; i++ {
		result[i] = make([]float64, colsB)
		for j := 0; j < colsB; j++ {
			sum := 0.0
			for k := 0; k < colsA; k++ {
				sum += a[i][k] * b[k][j]
			}
			result[i][j] = sum
		}
	}
	return result, nil
}

// MatrixDeterminant computes the determinant of a square matrix using
// recursive cofactor expansion
func (s *Service) MatrixDeterminant(m [][]float64) (float64, error) {
	n := len(m)
	if n == 0 {
		return 0, fmt.Errorf("matrix must be non-empty")
	}
	for _, row := range m {
		if len(row) != n {
			return 0, fmt.Errorf("matrix must be square")
		}
	}
	return matrixDeterminantRecursive(m), nil
}

// matrixDeterminantRecursive is the unexported recursive cofactor-expansion
// helper backing MatrixDeterminant
func matrixDeterminantRecursive(m [][]float64) float64 {
	n := len(m)
	if n == 1 {
		return m[0][0]
	}
	if n == 2 {
		return m[0][0]*m[1][1] - m[0][1]*m[1][0]
	}
	det := 0.0
	for col := 0; col < n; col++ {
		minor := make([][]float64, n-1)
		for i := 1; i < n; i++ {
			minorRow := make([]float64, 0, n-1)
			for j := 0; j < n; j++ {
				if j == col {
					continue
				}
				minorRow = append(minorRow, m[i][j])
			}
			minor[i-1] = minorRow
		}
		sign := 1.0
		if col%2 != 0 {
			sign = -1.0
		}
		det += sign * m[0][col] * matrixDeterminantRecursive(minor)
	}
	return det
}

// Sequence generates count numbers of the given sequence type starting at
// start; seqType "arithmetic" adds step each time, "geometric" multiplies by
// step each time
func (s *Service) Sequence(seqType string, start, step float64, count int) ([]float64, error) {
	if count < 0 {
		return nil, fmt.Errorf("count must be non-negative")
	}
	result := make([]float64, count)
	switch seqType {
	case "arithmetic":
		for i := 0; i < count; i++ {
			result[i] = start + step*float64(i)
		}
	case "geometric":
		value := start
		for i := 0; i < count; i++ {
			result[i] = value
			value *= step
		}
	default:
		return nil, fmt.Errorf("unknown sequence type %q (expected arithmetic or geometric)", seqType)
	}
	return result, nil
}
