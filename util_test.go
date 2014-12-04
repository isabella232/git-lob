package main

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("Util", func() {
	Describe("Size methods", func() {

		It("formats sizes", func() {

			var str string
			str = FormatSize(55)
			Expect(str).To(Equal("55"))
			str = FormatSize(1024)
			Expect(str).To(Equal("1KB"))
			str = FormatSize(2000)
			Expect(str).To(Equal("1.95KB"))
			str = FormatSize(1572864)
			Expect(str).To(Equal("1.5MB"))
			str = FormatSize(157286400)
			Expect(str).To(Equal("150MB"))
			str = FormatSize(44023414784)
			Expect(str).To(Equal("41GB"))
			str = FormatSize(44475414800)
			Expect(str).To(Equal("41.4GB"))
			str = FormatSize(1319413953331)
			Expect(str).To(Equal("1.2TB"))
			str = FormatSize(395824185999360)
			Expect(str).To(Equal("360TB"))
			str = FormatSize(2260595906707456)
			Expect(str).To(Equal("2.01PB"))

		})
		It("parses sizes", func() {
			var val int64
			var err error
			val, err = ParseSize("5a67")
			Expect(err).ToNot(BeNil(), "Should not parse")
			val, err = ParseSize("567")
			Expect(err).To(BeNil(), "Should parse without error")
			Expect(val).To(BeEquivalentTo(567))
			val, err = ParseSize("567B")
			Expect(err).To(BeNil(), "Should parse without error")
			Expect(val).To(BeEquivalentTo(567))
			val, err = ParseSize("567b")
			Expect(err).To(BeNil(), "Should parse without error")
			Expect(val).To(BeEquivalentTo(567))
			val, err = ParseSize(" 567 B ")
			Expect(err).To(BeNil(), "Should parse without error")
			Expect(val).To(BeEquivalentTo(567))
			val, err = ParseSize("1KB")
			Expect(err).To(BeNil(), "Should parse without error")
			Expect(val).To(BeEquivalentTo(1024))
			val, err = ParseSize("2.5KB")
			Expect(err).To(BeNil(), "Should parse without error")
			Expect(val).To(BeEquivalentTo(2560))
			val, err = ParseSize("5.25M")
			Expect(err).To(BeNil(), "Should parse without error")
			Expect(val).To(BeEquivalentTo(5505024))
			val, err = ParseSize("75.0Gb")
			Expect(err).To(BeNil(), "Should parse without error")
			Expect(val).To(BeEquivalentTo(80530636800))
			val, err = ParseSize("300T")
			Expect(err).To(BeNil(), "Should parse without error")
			Expect(val).To(BeEquivalentTo(329853488332800))
			val, err = ParseSize("1.5pb")
			Expect(err).To(BeNil(), "Should parse without error")
			Expect(val).To(BeEquivalentTo(1688849860263936))
		})

	})

	Describe("StringBinarySearch", func() {
		// Note capitalised ordering
		sortedSlice := []string{"Cheetah", "Frog", "aardvark", "bear", "cheetah", "dog", "elephant", "zebra"}

		It("behaves correctly on empty lists", func() {

			list := make([]string, 0)
			found, insertAt := StringBinarySearch(list, "test")
			Expect(found).To(BeFalse(), "Should not find in empty list")
			Expect(insertAt).To(BeEquivalentTo(0), "Empty list insertion should be zero")

		})
		It("behaves correctly on empty search term", func() {

			found, insertAt := StringBinarySearch(sortedSlice, "")
			Expect(found).To(BeFalse(), "Should not find empty string")
			Expect(insertAt).To(BeEquivalentTo(0), "Should insert empty string at start")

		})
		It("inserts at start", func() {

			found, insertAt := StringBinarySearch(sortedSlice, "Aardvark")
			Expect(found).To(BeFalse(), "Should not find string")
			Expect(insertAt).To(BeEquivalentTo(0), "Should insert string at start")

		})
		It("inserts at end", func() {

			found, insertAt := StringBinarySearch(sortedSlice, "zoltan")
			Expect(found).To(BeFalse(), "Should not find string")
			Expect(insertAt).To(BeEquivalentTo(len(sortedSlice)), "Should insert string at end")

		})
		It("inserts in middle", func() {

			found, insertAt := StringBinarySearch(sortedSlice, "Dingo")
			Expect(found).To(BeFalse(), "Should not find string")
			Expect(insertAt).To(BeEquivalentTo(1), "Should insert at correct point")

			found, insertAt = StringBinarySearch(sortedSlice, "anteater")
			Expect(found).To(BeFalse(), "Should not find string")
			Expect(insertAt).To(BeEquivalentTo(3), "Should insert at correct point")

			found, insertAt = StringBinarySearch(sortedSlice, "chaffinch")
			Expect(found).To(BeFalse(), "Should not find string")
			Expect(insertAt).To(BeEquivalentTo(4), "Should insert at correct point")

			found, insertAt = StringBinarySearch(sortedSlice, "fox")
			Expect(found).To(BeFalse(), "Should not find string")
			Expect(insertAt).To(BeEquivalentTo(7), "Should insert at correct point")

		})
		It("is case sensitive", func() {

			found, insertAt := StringBinarySearch(sortedSlice, "Dog")
			Expect(found).To(BeFalse(), "Should not find string")
			Expect(insertAt).To(BeEquivalentTo(1), "Should insert at correct point")

			found, insertAt = StringBinarySearch(sortedSlice, "frog")
			Expect(found).To(BeFalse(), "Should not find string")
			Expect(insertAt).To(BeEquivalentTo(7), "Should insert at correct point")

		})
		It("finds existing", func() {
			// Note not using loop and sortedSlice[i] to test for equality not identity
			found, at := StringBinarySearch(sortedSlice, "Cheetah")
			Expect(found).To(BeTrue(), "Should find string")
			Expect(at).To(BeEquivalentTo(0), "Should insert at correct point")
			found, at = StringBinarySearch(sortedSlice, "Frog")
			Expect(found).To(BeTrue(), "Should find string")
			Expect(at).To(BeEquivalentTo(1), "Should insert at correct point")
			found, at = StringBinarySearch(sortedSlice, "aardvark")
			Expect(found).To(BeTrue(), "Should find string")
			Expect(at).To(BeEquivalentTo(2), "Should insert at correct point")
			found, at = StringBinarySearch(sortedSlice, "bear")
			Expect(found).To(BeTrue(), "Should find string")
			Expect(at).To(BeEquivalentTo(3), "Should insert at correct point")
			found, at = StringBinarySearch(sortedSlice, "cheetah")
			Expect(found).To(BeTrue(), "Should find string")
			Expect(at).To(BeEquivalentTo(4), "Should insert at correct point")
			found, at = StringBinarySearch(sortedSlice, "dog")
			Expect(found).To(BeTrue(), "Should find string")
			Expect(at).To(BeEquivalentTo(5), "Should insert at correct point")
			found, at = StringBinarySearch(sortedSlice, "elephant")
			Expect(found).To(BeTrue(), "Should find string")
			Expect(at).To(BeEquivalentTo(6), "Should insert at correct point")
			found, at = StringBinarySearch(sortedSlice, "zebra")
			Expect(found).To(BeTrue(), "Should find string")
			Expect(at).To(BeEquivalentTo(7), "Should insert at correct point")
		})

	})

})
