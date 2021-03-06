// Copyright © 2018 Wei Shen <shenwei356@gmail.com>
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN
// THE SOFTWARE.

package cmd

import (
	"fmt"
	"io"
	"runtime"
	"sort"

	"github.com/shenwei356/bio/seq"
	"github.com/shenwei356/bio/seqio/fastx"
	"github.com/shenwei356/unikmer"
	"github.com/spf13/cobra"
)

// countCmd represents
var countCmd = &cobra.Command{
	Use:   "count",
	Short: "count k-mers from FASTA/Q sequences",
	Long: `count k-mers from FASTA/Q sequences

`,
	Run: func(cmd *cobra.Command, args []string) {
		opt := getOptions(cmd)
		runtime.GOMAXPROCS(opt.NumCPUs)
		seq.ValidateSeq = false

		var err error

		var files []string
		infileList := getFlagString(cmd, "infile-list")
		if infileList != "" {
			files, err = getListFromFile(infileList)
			checkError(err)
		} else {
			files = getFileList(args)
		}

		outFile := getFlagString(cmd, "out-prefix")
		circular := getFlagBool(cmd, "circular")
		k := getFlagPositiveInt(cmd, "kmer-len")
		if k > 32 {
			checkError(fmt.Errorf("k > 32 not supported"))
		}

		checkFiles("", files...)

		canonical := getFlagBool(cmd, "canonical")
		sortKmers := getFlagBool(cmd, "sort")

		if !isStdout(outFile) {
			outFile += extDataFile
		}
		outfh, gw, w, err := outStream(outFile, opt.Compress, opt.CompressionLevel)
		checkError(err)
		defer func() {
			outfh.Flush()
			if gw != nil {
				gw.Close()
			}
			w.Close()
		}()

		var mode uint32
		if opt.Compact {
			mode |= unikmer.UNIK_COMPACT
		}
		if canonical {
			mode |= unikmer.UNIK_CANONICAL
		}
		if sortKmers {
			mode |= unikmer.UNIK_SORTED
		}
		writer, err := unikmer.NewWriter(outfh, k, mode)
		checkError(err)
		m := make(map[uint64]struct{}, mapInitSize)

		var m2 []uint64
		if sortKmers {
			m2 = make([]uint64, 0, mapInitSize)
		}

		var sequence, kmer, preKmer []byte
		var originalLen, l, end, e int
		var record *fastx.Record
		var fastxReader *fastx.Reader
		var kcode, preKcode unikmer.KmerCode
		var first bool
		var i, j, iters int
		var ok bool
		var n int64
		for _, file := range files {
			if opt.Verbose {
				log.Infof("reading sequence file: %s", file)
			}
			fastxReader, err = fastx.NewDefaultReader(file)
			checkError(err)
			for {
				record, err = fastxReader.Read()
				if err != nil {
					if err == io.EOF {
						break
					}
					checkError(err)
					break
				}

				if canonical {
					iters = 1
				} else {
					iters = 2
				}

				for j = 0; j < iters; j++ {
					if j == 0 { // sequence
						sequence = record.Seq.Seq

						if opt.Verbose {
							log.Infof("processing sequence: %s", record.ID)
						}
					} else { // reverse complement sequence
						sequence = record.Seq.RevComInplace().Seq

						if opt.Verbose {
							log.Infof("processing reverse complement sequence: %s", record.ID)
						}
					}

					originalLen = len(record.Seq.Seq)
					l = len(sequence)

					end = l - 1
					if end < 0 {
						end = 0
					}
					first = true
					for i = 0; i <= end; i++ {
						e = i + k
						if e > originalLen {
							if circular {
								e = e - originalLen
								kmer = sequence[i:]
								kmer = append(kmer, sequence[0:e]...)
							} else {
								break
							}
						} else {
							kmer = sequence[i : i+k]
						}

						if first {
							kcode, err = unikmer.NewKmerCode(kmer)
							first = false
						} else {
							kcode, err = unikmer.NewKmerCodeMustFromFormerOne(kmer, preKmer, preKcode)
						}
						if err != nil {
							checkError(fmt.Errorf("fail to encode '%s': %s", kmer, err))
						}
						preKmer, preKcode = kmer, kcode

						if canonical {
							kcode = kcode.Canonical()
						}

						if _, ok = m[kcode.Code]; !ok {
							m[kcode.Code] = struct{}{}
							if sortKmers {
								m2 = append(m2, kcode.Code)
							} else {
								checkError(writer.Write(kcode))
								n++
							}
						}
					}
				}
			}
		}
		if sortKmers {
			n = int64(len(m2))

			if opt.Verbose {
				log.Infof("sorting %d k-mers", n)
			}
			sort.Sort(unikmer.CodeSlice(m2))
			if opt.Verbose {
				log.Infof("done sorting")
			}
			writer.Number = n

			for _, code := range m2 {
				writer.Write(unikmer.KmerCode{Code: code, K: k})
			}
		}

		checkError(writer.Flush())
		if opt.Verbose {
			log.Infof("%d unique k-mers saved", n)
		}
	},
}

func init() {
	RootCmd.AddCommand(countCmd)

	countCmd.Flags().StringP("out-prefix", "o", "-", `out file prefix ("-" for stdout)`)
	countCmd.Flags().IntP("kmer-len", "k", 0, "k-mer length")
	countCmd.Flags().BoolP("circular", "", false, "circular genome")
	countCmd.Flags().BoolP("canonical", "K", false, "only keep the canonical k-mers")
	countCmd.Flags().BoolP("sort", "s", false, helpSort)
}
