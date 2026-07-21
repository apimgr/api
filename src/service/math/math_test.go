package math

import (
	"math"
	"math/big"
	"testing"

	"github.com/stretchr/testify/assert"
)

// Basic arithmetic operations: happy path plus boundary values
// (zero, negative) for each operator.
func TestBasicOperations(t *testing.T) {
	s := New()

	assert.Equal(t, 5.0, s.Add(2, 3))
	assert.Equal(t, -1.0, s.Add(-3, 2))
	assert.Equal(t, 0.0, s.Add(0, 0))

	assert.Equal(t, -1.0, s.Subtract(2, 3))
	assert.Equal(t, 5.0, s.Subtract(2, -3))

	assert.Equal(t, 6.0, s.Multiply(2, 3))
	assert.Equal(t, 0.0, s.Multiply(0, 100))
	assert.Equal(t, -6.0, s.Multiply(2, -3))
}

// Divide must error on division by zero and otherwise compute correctly,
// including negative operands.
func TestDivide(t *testing.T) {
	s := New()

	got, err := s.Divide(6, 3)
	assert.NoError(t, err)
	assert.Equal(t, 2.0, got)

	got, err = s.Divide(-6, 3)
	assert.NoError(t, err)
	assert.Equal(t, -2.0, got)

	_, err = s.Divide(6, 0)
	assert.ErrorIs(t, err, ErrDivisionByZero)
}

// Modulo must error on division by zero and otherwise match Go's %.
func TestModulo(t *testing.T) {
	s := New()

	got, err := s.Modulo(10, 3)
	assert.NoError(t, err)
	assert.Equal(t, int64(1), got)

	got, err = s.Modulo(-10, 3)
	assert.NoError(t, err)
	assert.Equal(t, int64(-10%3), got)

	_, err = s.Modulo(10, 0)
	assert.ErrorIs(t, err, ErrDivisionByZero)
}

func TestPower(t *testing.T) {
	s := New()

	assert.Equal(t, 8.0, s.Power(2, 3))
	assert.Equal(t, 1.0, s.Power(5, 0))
	assert.Equal(t, 0.5, s.Power(2, -1))
}

// SquareRoot must error on negative input.
func TestSquareRoot(t *testing.T) {
	s := New()

	got, err := s.SquareRoot(9)
	assert.NoError(t, err)
	assert.Equal(t, 3.0, got)

	got, err = s.SquareRoot(0)
	assert.NoError(t, err)
	assert.Equal(t, 0.0, got)

	_, err = s.SquareRoot(-4)
	assert.ErrorIs(t, err, ErrNegativeSquareRoot)
}

func TestCubeRoot(t *testing.T) {
	s := New()

	assert.InDelta(t, 2.0, s.CubeRoot(8), 1e-9)
	assert.InDelta(t, -2.0, s.CubeRoot(-8), 1e-9)
	assert.Equal(t, 0.0, s.CubeRoot(0))
}

// Trig functions and their inverses are compared against math package
// directly (they are one-line delegations, but verify wiring and units).
func TestTrigFunctions(t *testing.T) {
	s := New()

	assert.InDelta(t, math.Sin(1.2), s.Sin(1.2), 1e-12)
	assert.InDelta(t, math.Cos(1.2), s.Cos(1.2), 1e-12)
	assert.InDelta(t, math.Tan(1.2), s.Tan(1.2), 1e-12)
	assert.InDelta(t, math.Asin(0.5), s.Asin(0.5), 1e-12)
	assert.InDelta(t, math.Acos(0.5), s.Acos(0.5), 1e-12)
	assert.InDelta(t, math.Atan(0.5), s.Atan(0.5), 1e-12)
}

func TestAngleConversions(t *testing.T) {
	s := New()

	assert.InDelta(t, math.Pi, s.DegreesToRadians(180), 1e-12)
	assert.InDelta(t, 0.0, s.DegreesToRadians(0), 1e-12)
	assert.InDelta(t, 180.0, s.RadiansToDegrees(math.Pi), 1e-12)
	assert.InDelta(t, 0.0, s.RadiansToDegrees(0), 1e-12)
}

func TestLogarithmicFunctions(t *testing.T) {
	s := New()

	assert.InDelta(t, 0.0, s.Log(1), 1e-12)
	assert.InDelta(t, 2.0, s.Log10(100), 1e-12)
	assert.InDelta(t, 3.0, s.Log2(8), 1e-12)
	assert.InDelta(t, 1.0, s.Exp(0), 1e-12)
	assert.True(t, math.IsInf(s.Log(0), -1))
	assert.True(t, math.IsNaN(s.Log(-1)))
}

func TestRoundingFunctions(t *testing.T) {
	s := New()

	assert.Equal(t, 3.0, s.Round(2.5))
	assert.Equal(t, -3.0, s.Round(-2.5))
	assert.Equal(t, 2.0, s.Floor(2.9))
	assert.Equal(t, -3.0, s.Floor(-2.1))
	assert.Equal(t, 3.0, s.Ceil(2.1))
	assert.Equal(t, -2.0, s.Ceil(-2.9))
	assert.Equal(t, 5.0, s.Abs(-5))
	assert.Equal(t, 5.0, s.Abs(5))
	assert.Equal(t, 0.0, s.Abs(0))
}

// Statistical functions: happy path, single element, and empty-slice
// boundary (must return zero, not panic).
func TestStatisticalFunctions(t *testing.T) {
	s := New()

	nums := []float64{3, 1, 4, 1, 5, 9, 2, 6}
	assert.Equal(t, 1.0, s.Min(nums))
	assert.Equal(t, 9.0, s.Max(nums))
	assert.Equal(t, 31.0, s.Sum(nums))
	assert.InDelta(t, 31.0/8.0, s.Average(nums), 1e-12)

	assert.Equal(t, 0.0, s.Min(nil))
	assert.Equal(t, 0.0, s.Max(nil))
	assert.Equal(t, 0.0, s.Sum(nil))
	assert.Equal(t, 0.0, s.Average(nil))

	single := []float64{42}
	assert.Equal(t, 42.0, s.Min(single))
	assert.Equal(t, 42.0, s.Max(single))
	assert.Equal(t, 42.0, s.Average(single))
}

// Median covers odd-length, even-length, empty, and single-element
// inputs, plus an already-sorted and a reverse-sorted input to exercise
// the bubble sort.
func TestMedian(t *testing.T) {
	s := New()

	assert.Equal(t, 3.0, s.Median([]float64{1, 2, 3, 4, 5}))
	assert.Equal(t, 2.5, s.Median([]float64{1, 2, 3, 4}))
	assert.Equal(t, 0.0, s.Median(nil))
	assert.Equal(t, 7.0, s.Median([]float64{7}))
	assert.Equal(t, 3.0, s.Median([]float64{5, 4, 3, 2, 1}))
}

// Factorial covers 0! and 1! boundary cases plus a value large enough to
// require big.Int (beyond int64 range) to confirm no overflow.
func TestFactorial(t *testing.T) {
	s := New()

	assert.Equal(t, big.NewInt(1), s.Factorial(0))
	assert.Equal(t, big.NewInt(1), s.Factorial(1))
	assert.Equal(t, big.NewInt(120), s.Factorial(5))

	got := s.Factorial(25)
	want, ok := new(big.Int).SetString("15511210043330985984000000", 10)
	assert.True(t, ok)
	assert.Equal(t, want, got)
}

// IsPrime covers negatives, 0, 1, 2 (smallest prime), even numbers, and
// a larger prime/composite pair.
func TestIsPrime(t *testing.T) {
	s := New()

	cases := []struct {
		n    int64
		want bool
	}{
		{-5, false},
		{0, false},
		{1, false},
		{2, true},
		{3, true},
		{4, false},
		{17, true},
		{97, true},
		{100, false},
		{7919, true},
	}
	for _, c := range cases {
		assert.Equal(t, c.want, s.IsPrime(c.n), "IsPrime(%d)", c.n)
	}
}

// GCD/LCM cover zero operands, equal operands, and coprime pairs.
func TestGCDAndLCM(t *testing.T) {
	s := New()

	assert.Equal(t, int64(6), s.GCD(54, 24))
	assert.Equal(t, int64(5), s.GCD(5, 0))
	assert.Equal(t, int64(1), s.GCD(17, 13))

	assert.Equal(t, int64(216), s.LCM(54, 24))
	assert.Equal(t, int64(221), s.LCM(17, 13))
}

// RandomInt and RandomFloat must stay within the requested bounds across
// many draws (statistical sanity, not just single draw luck).
func TestRandomBounds(t *testing.T) {
	s := New()

	for i := 0; i < 200; i++ {
		n := s.RandomInt(5, 10)
		assert.GreaterOrEqual(t, n, int64(5))
		assert.LessOrEqual(t, n, int64(10))

		f := s.RandomFloat(-1.5, 1.5)
		assert.GreaterOrEqual(t, f, -1.5)
		assert.LessOrEqual(t, f, 1.5)
	}

	// Degenerate range: min == max must always return that value.
	assert.Equal(t, int64(7), s.RandomInt(7, 7))
}

// Percentage helpers cover the whole-is-zero boundary explicitly called
// out in the source to avoid a division by zero.
func TestPercentageFunctions(t *testing.T) {
	s := New()

	assert.Equal(t, 50.0, s.PercentageOf(5, 10))
	assert.Equal(t, 0.0, s.PercentageOf(5, 0))
	assert.Equal(t, 0.0, s.PercentageOf(0, 10))

	assert.Equal(t, 100.0, s.PercentageChange(10, 20))
	assert.Equal(t, -50.0, s.PercentageChange(10, 5))
	assert.Equal(t, 0.0, s.PercentageChange(0, 20))
}
