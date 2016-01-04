package video

import "image"

var color_channel = [12]int{0, 0, 0, 0, 1, 2, 1, 2, 1, 2, 1, 2}

type Macroblock struct {
	macroblock_address_increment uint32
	macroblock_type              *MacroblockType
	spatial_temporal_weight_code uint32
	frame_motion_type            uint32
	field_motion_type            uint32
	dct_type                     bool
	quantiser_scale_code         uint32

	cpb int
}

func (br *VideoSequence) macroblock(mbAddress int, frameSlice *image.YCbCr) (int, error) {

	mb := Macroblock{}

	for {
		if nextbits, err := br.Peek32(11); err != nil {
			return 0, err
		} else if nextbits == 0x08 { // 0000 0001 000
			br.Trash(11)
			mb.macroblock_address_increment += 33
		} else {
			break
		}
	}

	if incr, err := macroblockAddressIncrementDecoder.Decode(br); err != nil {
		return 0, err
	} else {
		mb.macroblock_address_increment += incr
	}

	if mb.macroblock_address_increment > 1 {
		br.resetDCPredictors()
	}

	mbAddress += int(mb.macroblock_address_increment)

	if err := br.macroblock_mode(&mb); err != nil {
		return 0, err
	}

	if mb.macroblock_type.macroblock_intra == false {
		br.resetDCPredictors()
	}

	if mb.macroblock_type.macroblock_quant {
		if qsc, err := br.Read32(5); err != nil {
			return 0, err
		} else {
			mb.quantiser_scale_code = qsc
			br.currentQSC = qsc
		}
	}

	var mvd motionVectorData

	if mb.macroblock_type.macroblock_motion_forward ||
		(mb.macroblock_type.macroblock_intra && br.PictureCodingExtension.concealment_motion_vectors) {
		if err := br.motion_vectors(0, &mvd, &mb); err != nil {
			return 0, err
		}
	}

	if mb.macroblock_type.macroblock_motion_backward {
		if err := br.motion_vectors(1, &mvd, &mb); err != nil {
			return 0, err
		}
	}

	if mb.macroblock_type.macroblock_intra && br.PictureCodingExtension.concealment_motion_vectors {
		if err := marker_bit(br); err != nil {
			return 0, err
		}
	}

	if mb.macroblock_type.macroblock_pattern {
		if cpb, err := coded_block_pattern(br, br.SequenceExtension.chroma_format); err != nil {
			return 0, nil
		} else {
			mb.cpb = cpb
		}
	}

	var block_count int
	switch br.SequenceExtension.chroma_format {
	case ChromaFormat_420:
		block_count = 6
	case ChromaFormat_422:
		block_count = 8
	case ChromaFormat_444:
		block_count = 12
	}

	pattern_code := mb.decodePatternCode(br.SequenceExtension.chroma_format)

	var b block
	var decoded block

	for i := 0; i < block_count; i++ {
		cc := color_channel[i]

		if pattern_code[i] {
			if err := br.block(cc, &b, &mb); err != nil {
				return 0, err
			}
		}

		err := br.decode_block(cc, &b, &decoded, mb.macroblock_type.macroblock_intra)
		if err != nil {
			return 0, err
		}
		idct(&decoded)
		updateFrameSlice(i, mbAddress, frameSlice, &decoded)
	}

	return mbAddress, nil
}

type clampedBlock [blockSize]uint8

func clamp(dest *clampedBlock, src *block) {
	for i := 0; i < 64; i++ {
		if src[i] > 255 {
			dest[i] = 255
		} else if src[i] < 0 {
			dest[i] = 0
		} else {
			dest[i] = uint8(src[i])
		}
	}
}

func updateFrameSlice(i int, mbAddress int, frameSlice *image.YCbCr, b *block) {

	var cb clampedBlock
	clamp(&cb, b)

	switch i {
	case 0:
		xs := (mbAddress - 1) * 16
		for y := 0; y < 8; y++ {
			si := y * 8
			di := y*frameSlice.YStride + xs
			copy(frameSlice.Y[di:di+8], cb[si:si+8])
		}
	case 1:
		xs := (mbAddress - 1) * 16
		for y := 0; y < 8; y++ {
			si := y * 8
			di := y*frameSlice.YStride + (xs + 8)
			copy(frameSlice.Y[di:di+8], cb[si:si+8])
		}
	case 2:
		xs := (mbAddress - 1) * 16
		for y := 0; y < 8; y++ {
			si := y * 8
			di := (y+8)*frameSlice.YStride + xs
			copy(frameSlice.Y[di:di+8], cb[si:si+8])
		}
	case 3:
		xs := (mbAddress - 1) * 16
		for y := 0; y < 8; y++ {
			si := y * 8
			di := (y+8)*frameSlice.YStride + (xs + 8)
			copy(frameSlice.Y[di:di+8], cb[si:si+8])
		}
	case 4:
		xs := (mbAddress - 1) * 8
		for y := 0; y < 8; y++ {
			si := y * 8
			di := y*frameSlice.CStride + xs
			copy(frameSlice.Cb[di:di+8], cb[si:si+8])
		}
	case 5:
		xs := (mbAddress - 1) * 8
		for y := 0; y < 8; y++ {
			si := y * 8
			di := y*frameSlice.CStride + xs
			copy(frameSlice.Cr[di:di+8], cb[si:si+8])
		}
	}

}

type PatternCode [12]bool

func (mb *Macroblock) decodePatternCode(chroma_format chromaFormat) (pattern_code PatternCode) {
	for i := 0; i < 12; i++ {
		if mb.macroblock_type.macroblock_intra {
			pattern_code[i] = true
		} else {
			pattern_code[i] = false
		}
	}

	if mb.macroblock_type.macroblock_pattern {
		for i := 0; i < 6; i++ {
			mask := 1 << uint(5-i)
			if mb.cpb&mask == mask {
				pattern_code[i] = true
			}
		}

		if chroma_format == ChromaFormat_422 || chroma_format == ChromaFormat_444 {
			panic("unsupported: coded block pattern chroma format")
		}
	}

	return
}

func (br *VideoSequence) macroblock_mode(mb *Macroblock) (err error) {

	var typeDecoder macroblockTypeDecoderFn
	switch br.PictureHeader.picture_coding_type {
	case IntraCoded:
		typeDecoder = macroblockTypeDecoder.IFrame
	case PredictiveCoded:
		typeDecoder = macroblockTypeDecoder.PFrame
	case BidirectionallyPredictiveCoded:
		typeDecoder = macroblockTypeDecoder.BFrame
	default:
		panic("not implemented: macroblock type decoder")
	}

	mb.macroblock_type, err = typeDecoder(br)
	if err != nil {
		return err
	}

	if mb.macroblock_type.spatial_temporal_weight_code_flag &&
		false /* ( spatial_temporal_weight_code_table_index != ‘00’) */ {
		mb.spatial_temporal_weight_code, err = br.Read32(2)
		if err != nil {
			return err
		}
	}

	if mb.macroblock_type.macroblock_motion_forward ||
		mb.macroblock_type.macroblock_motion_backward {
		if br.PictureCodingExtension.picture_structure == PictureStructure_FramePicture {
			if br.PictureCodingExtension.frame_pred_frame_dct == 0 {
				mb.frame_motion_type, err = br.Read32(2)
				if err != nil {
					return err
				}
			}
		} else {
			mb.field_motion_type, err = br.Read32(2)
			if err != nil {
				return err
			}
		}
	}

	if br.PictureCodingExtension.picture_structure == PictureStructure_FramePicture &&
		br.PictureCodingExtension.frame_pred_frame_dct == 0 &&
		(mb.macroblock_type.macroblock_intra || mb.macroblock_type.macroblock_pattern) {
		mb.dct_type, err = br.ReadBit() //dct_type 1 uimsbf
		if err != nil {
			return err
		}
	}

	return nil
}
