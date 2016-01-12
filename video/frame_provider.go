package video

import "errors"
import "image"
import "os"

var EOS = errors.New("end of sequence")
var ErrUnsupportedVideoStream_ISO_IEC_11172_2 = errors.New("unsupported video stream ISO/IEC 11172-2")

func (self *VideoSequence) Next() (image.Image, uint32, error) {

	if self.SequenceHeader != nil {
		goto RESUME
	}

	// align to next start code
	if err := next_start_code(self); err != nil {
		panic(err)
	}

	// read sequence_header
	if err := self.sequence_header(); err != nil {
		panic(err)
	}

	// peek for sequence_extension
	if val, err := self.Peek32(32); err != nil {
		panic(err)
	} else if StartCode(val) != ExtensionStartCode {
		// Stream is MPEG-1 Video
		return nil, 0, ErrUnsupportedVideoStream_ISO_IEC_11172_2
	}

	if err := self.sequence_extension(); err != nil {
		panic(err)
	}

CONTINUE:

	if err := self.extension_and_user_data(0); err != nil {
		panic("extension_and_user_data: " + err.Error())
	}

MORE_FRAMES:

	if nextbits, err := self.Peek32(32); err != nil {
		panic("Peek32")
	} else if StartCode(nextbits) == GroupStartCode {
		os.Stdout.WriteString("\n")
		if err := self.group_of_pictures_header(); err != nil {
			panic("group_of_pictures_header: " + err.Error())
		}
		if err := self.extension_and_user_data(1); err != nil {
			panic("extension_and_user_data:" + err.Error())
		}
	}

	if err := self.picture_header(); err != nil {
		panic("picture_header: " + err.Error())
	}
	os.Stdout.WriteString(self.PictureHeader.String())

	if err := self.picture_coding_extension(); err != nil {
		panic("picture_coding_extension: " + err.Error())
	}

	if err := self.extension_and_user_data(2); err != nil {
		panic("extension_and_user_data: " + err.Error())
	}

	self.frameStore.set(self.PictureHeader.temporal_reference)

	if frame, err := self.picture_data(); err != nil {
		panic(err)
	} else {
		switch self.PictureHeader.picture_coding_type {
		case IFrame, PFrame:
			self.frameStore.add(frame, self.PictureHeader.temporal_reference)
		}
		return frame, self.PictureHeader.temporal_reference, nil
	}

RESUME:

	if nextbits, err := self.Peek32(32); err != nil {
		panic("peeking: " + err.Error())
	} else if StartCode(nextbits) == PictureStartCode {
		goto MORE_FRAMES
	} else if StartCode(nextbits) == GroupStartCode {
		goto MORE_FRAMES
	}

	if nextbits, err := self.Peek32(32); err != nil {
		panic("Peek32")
	} else if StartCode(nextbits) == SequenceEndStartCode {
		// consume SequenceEndStartCode
		if err := self.Trash(32); err != nil {
			return nil, 0, err
		}
		return nil, 0, EOS
	}

	if err := self.sequence_header(); err != nil {
		panic(err)
	}

	if err := self.sequence_extension(); err != nil {
		panic(err)
	}

	goto CONTINUE
}
