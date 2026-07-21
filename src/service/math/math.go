package math

import (
	"fmt"
	"math"
	"math/big"
	"math/rand"
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
	return (a * b) / s.GCD(a, b)
}

// Random numbers
func (s *Service) RandomInt(min, max int64) int64 {
	return min + rand.Int63n(max-min+1)
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
