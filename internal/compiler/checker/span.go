package checker

import "github.com/norunners/tue/internal/compiler/sfc"

func spanWithin(base sfc.Span, source string, start int, end int) sfc.Span {
	if start < 0 {
		start = 0
	}
	if end < start {
		end = start
	}
	if start > len(source) {
		start = len(source)
	}
	if end > len(source) {
		end = len(source)
	}

	return sfc.Span{
		Start: positionWithin(base.Start, source, start),
		End:   positionWithin(base.Start, source, end),
	}
}

func positionWithin(base sfc.Position, source string, offset int) sfc.Position {
	if offset < 0 {
		offset = 0
	}
	if offset > len(source) {
		offset = len(source)
	}

	position := sfc.Position{
		Offset: base.Offset + offset,
		Line:   base.Line,
		Column: base.Column,
	}
	for index := 0; index < offset; index++ {
		if source[index] == '\n' {
			position.Line++
			position.Column = 1
			continue
		}
		position.Column++
	}
	return position
}
