// Copyright ©2013 The gonum Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.
// Based on the QRDecomposition class from Jama 1.0.3.

package mat64

import (
	"math"

	"github.com/gonum/blas"
	"github.com/gonum/blas/blas64"
	"github.com/gonum/lapack/lapack64"
)

// QR is a type for creating and using the QR factorization of a matrix.
type QR struct {
	qr  *Dense
	tau []float64
}

// Factorize computes the QR factorization of an m×n matrix a where m >= n. The QR
// factorization always exists even if A is singular.
//
// The QR decomposition is a factorization of the matrix A such that A = Q * R.
// The matrix Q is an orthonormal m×m matrix, and R is an m×n upper triangular matrix.
// Q and R can be extracted from the QFromQR and RFromQR methods on Dense.
func (qr *QR) Factorize(a Matrix) {
	m, n := a.Dims()
	if m < n {
		panic(ErrShape)
	}
	k := min(m, n)
	if qr.qr == nil {
		qr.qr = &Dense{}
	}
	qr.qr.Clone(a)
	work := make([]float64, 1)
	qr.tau = make([]float64, k)
	lapack64.Geqrf(qr.qr.mat, qr.tau, work, -1)

	work = make([]float64, int(work[0]))
	lapack64.Geqrf(qr.qr.mat, qr.tau, work, len(work))
}

// TODO(btracey): Add in the "Reduced" forms for extracting the n×n orthogonal
// and upper triangular matrices.

// RFromQR extracts the m×n upper trapezoidal matrix from a QR decomposition.
func (m *Dense) RFromQR(qr *QR) {
	r, c := qr.qr.Dims()
	m.reuseAs(r, c)

	// Disguise the QR as an upper triangular
	t := &TriDense{
		blas64.Triangular{
			N:      c,
			Stride: qr.qr.mat.Stride,
			Data:   qr.qr.mat.Data,
			Uplo:   blas.Upper,
			Diag:   blas.NonUnit,
		},
	}
	m.Copy(t)
}

// QFromQR extracts the m×m orthonormal matrix Q from a QR decomposition.
func (m *Dense) QFromQR(qr *QR) {
	r, c := qr.qr.Dims()
	m.reuseAs(r, r)

	// Set Q = I.
	for i := 0; i < r; i++ {
		for j := 0; j < i; j++ {
			m.mat.Data[i*m.mat.Stride+j] = 0
		}
		m.mat.Data[i*m.mat.Stride+i] = 1
		for j := i + 1; j < r; j++ {
			m.mat.Data[i*m.mat.Stride+j] = 0
		}
	}

	// Construct Q from the elementary reflectors.
	h := blas64.General{
		Rows:   r,
		Cols:   r,
		Stride: r,
		Data:   make([]float64, r*r),
	}
	qCopy := getWorkspace(r, r, false)
	v := blas64.Vector{
		Inc:  1,
		Data: make([]float64, r),
	}
	k := min(r, c)
	for i := 0; i < k; i++ {
		// Set h = I.
		for i := range h.Data {
			h.Data[i] = 0
		}
		for j := 0; j < r; j++ {
			h.Data[j*r+j] = 1
		}

		// Set the vector data as the elementary reflector.
		for j := 0; j < i; j++ {
			v.Data[j] = 0
		}
		v.Data[i] = 1
		for j := i + 1; j < r; j++ {
			v.Data[j] = qr.qr.mat.Data[j*qr.qr.mat.Stride+i]
		}

		// Compute the multiplication matrix.
		blas64.Ger(-qr.tau[i], v, v, h)
		qCopy.Copy(m)
		blas64.Gemm(blas.NoTrans, blas.NoTrans,
			1, qCopy.mat, h,
			0, m.mat)
	}
}

// SolveQR solves a minimum-norm solution to a system of linear equations defined
// by the matrices A and B, where A is an m×n matrix represented in its QR factorized
// form. If A is singular or near-singular a Condition error is returned. Please
// see the documentation for Condition for more information.
//
// The minimization problem solved depends on the input parameters.
//  1. If m >= n and trans == false, find X such that || A*X - B||_2 is minimized.
//  2. If m < n and trans == false, find the minimum norm solution of A * X = B.
//  3. If m >= n and trans == true, find the minimum norm solution of A^T * X = B.
//  4. If m < n and trans == true, find X such that || A*X - B||_2 is minimized.
// The solution matrix, X, is stored in place into the receiver.
func (m *Dense) SolveQR(qr *QR, trans bool, b Matrix) error {
	r, c := qr.qr.Dims()
	br, bc := b.Dims()

	// The QR solve algorithm stores the result in-place into B. The storage
	// for the answer must be large enough to hold both B and X. However,
	// the receiver must be the size of x. Copy B, and then copy into m at the
	// end.
	if trans {
		if c != br {
			panic(ErrShape)
		}
		m.reuseAs(r, bc)
	} else {
		if r != br {
			panic(ErrShape)
		}
		m.reuseAs(c, bc)
	}
	// Do not need to worry about overlap between m and b because x has its own
	// independent storage.
	x := getWorkspace(max(r, c), bc, false)
	x.Copy(b)
	t := blas64.Triangular{
		N:      qr.qr.mat.Cols,
		Stride: qr.qr.mat.Stride,
		Data:   qr.qr.mat.Data,
		Uplo:   blas.Upper,
		Diag:   blas.NonUnit,
	}
	if trans {
		ok := lapack64.Trtrs(blas.Trans, t, x.mat)
		if !ok {
			return Condition(math.Inf(1))
		}
		for i := c; i < r; i++ {
			for j := 0; j < bc; j++ {
				x.mat.Data[i*x.mat.Stride+j] = 0
			}
		}
		work := make([]float64, 1)
		lapack64.Ormqr(blas.Left, blas.NoTrans, qr.qr.mat, qr.tau, x.mat, work, -1)
		work = make([]float64, int(work[0]))
		lapack64.Ormqr(blas.Left, blas.NoTrans, qr.qr.mat, qr.tau, x.mat, work, len(work))
	} else {
		work := make([]float64, 1)
		lapack64.Ormqr(blas.Left, blas.Trans, qr.qr.mat, qr.tau, x.mat, work, -1)
		work = make([]float64, int(work[0]))
		lapack64.Ormqr(blas.Left, blas.Trans, qr.qr.mat, qr.tau, x.mat, work, len(work))

		ok := lapack64.Trtrs(blas.NoTrans, t, x.mat)
		if !ok {
			return Condition(math.Inf(1))
		}
	}
	// M was set above to be the correct size for the result.
	m.Copy(x)
	putWorkspace(x)
	return nil
}

func (v *Vector) SolveQRVec(qr *QR, trans bool, b *Vector) error {
	r, c := qr.qr.Dims()
	// The Solve implementation is non-trivial, so rather than duplicate the code,
	// instead recast the Vectors as Dense and call the matrix code.
	if trans {
		v.reuseAs(r)
	} else {
		v.reuseAs(c)
	}
	m := vecAsDense(v)
	bm := vecAsDense(b)
	return m.SolveQR(qr, trans, bm)
}

// vecAsDense returns the vector as a Dense matrix with the same underlying data.
func vecAsDense(v *Vector) *Dense {
	return &Dense{
		mat: blas64.General{
			Rows:   v.n,
			Cols:   1,
			Stride: v.mat.Inc,
			Data:   v.mat.Data,
		},
		capRows: v.n,
		capCols: 1,
	}
}
