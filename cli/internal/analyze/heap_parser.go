package analyze

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"
)

func (p *hprofParser) parse() error {
	file, err := os.Open(p.path)
	if err != nil {
		return err
	}
	defer file.Close()

	reader := bufio.NewReaderSize(file, 128*1024)
	if err := p.readHeader(reader); err != nil {
		return err
	}
	for {
		tag, length, err := readHprofRecordHeader(reader)
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("read HPROF record: %w", err)
		}
		recordLength, err := checkedInt64(uint64(length), "HPROF record length")
		if err != nil {
			return err
		}
		limited := &hprofReader{
			r:     io.LimitReader(reader, recordLength),
			limit: uint64(length),
		}
		switch tag {
		case hprofTagString:
			if err := p.parseStringRecord(limited, length); err != nil {
				return fmt.Errorf("parse HPROF string record in %s: %w", p.path, err)
			}
		case hprofTagLoadClass:
			if err := p.parseLoadClassRecord(limited); err != nil {
				return fmt.Errorf("parse HPROF class mapping in %s: %w", p.path, err)
			}
		case hprofTagHeapDump, hprofTagHeapDumpSeg:
			if err := p.parseHeapDump(limited, length); err != nil {
				return fmt.Errorf("parse HPROF heap record in %s: %w", p.path, err)
			}
		}
		if err := limited.skip(limited.remaining()); err != nil {
			return fmt.Errorf("consume HPROF record 0x%02x in %s: %w", tag, p.path, err)
		}
	}
	return p.resolveDeferredInstances()
}

func (p *hprofParser) readHeader(reader *bufio.Reader) error {
	var header []byte
	for {
		b, err := reader.ReadByte()
		if err != nil {
			return fmt.Errorf("read HPROF header: %w", err)
		}
		if b == 0 {
			break
		}
		header = append(header, b)
		if len(header) > 128 {
			return fmt.Errorf("read HPROF header: missing terminator")
		}
	}
	if !strings.HasPrefix(string(header), "JAVA PROFILE ") {
		return fmt.Errorf("%s не является Java HPROF дампом памяти", p.path)
	}
	idSizeRaw, err := readU4(reader)
	if err != nil {
		return fmt.Errorf("read HPROF id size: %w", err)
	}
	p.idSize, err = checkedInt(uint64(idSizeRaw), "HPROF id size")
	if err != nil {
		return err
	}
	if p.idSize <= 0 || p.idSize > 8 {
		return fmt.Errorf("unsupported HPROF id size %d", p.idSize)
	}
	if _, err := readU8(reader); err != nil {
		return fmt.Errorf("read HPROF timestamp: %w", err)
	}
	return nil
}

func readHprofRecordHeader(reader *bufio.Reader) (byte, uint32, error) {
	tag, err := reader.ReadByte()
	if err != nil {
		return 0, 0, err
	}
	if _, err := readU4(reader); err != nil {
		return 0, 0, err
	}
	length, err := readU4(reader)
	if err != nil {
		return 0, 0, err
	}
	return tag, length, nil
}

func (p *hprofParser) parseStringRecord(reader *hprofReader, length uint32) error {
	idSize := uint64(p.idSize)
	if idSize > uint64(length) {
		return fmt.Errorf("invalid HPROF string record length %d", length)
	}
	id, err := reader.readID(p.idSize)
	if err != nil {
		return err
	}
	dataLength := uint64(length) - idSize
	if err := reader.require(dataLength); err != nil {
		return fmt.Errorf("invalid HPROF string payload: %w", err)
	}
	_, exists := p.strings[id]
	if !exists && len(p.strings) >= p.limits.strings {
		p.degrade("strings", fmt.Sprintf(
			"Достигнут лимит строк HPROF (%d): последующие строки прочитаны, но не сохранены.",
			p.limits.strings,
		))
		return reader.skip(dataLength)
	}
	if dataLength > p.limits.stringRecordBytes {
		p.degrade("string-record-bytes", fmt.Sprintf(
			"Строка HPROF превышает безопасный лимит одной записи (%d байт): значение прочитано, но не сохранено.",
			p.limits.stringRecordBytes,
		))
		return reader.skip(dataLength)
	}
	previousSize := uint64(len(p.strings[id]))
	totalWithoutPrevious := p.stringBytes
	if previousSize <= totalWithoutPrevious {
		totalWithoutPrevious -= previousSize
	}
	totalStringBytes, err := checkedAddUint64(totalWithoutPrevious, dataLength, "HPROF string storage")
	if err != nil || totalStringBytes > p.limits.stringBytes {
		p.degrade("string-bytes", fmt.Sprintf(
			"Достигнут общий лимит памяти строк HPROF (%d байт): последующие значения прочитаны, но не сохранены.",
			p.limits.stringBytes,
		))
		return reader.skip(dataLength)
	}
	dataSize, err := checkedInt(dataLength, "HPROF string payload length")
	if err != nil {
		return err
	}
	data := make([]byte, dataSize)
	if _, err := io.ReadFull(reader, data); err != nil {
		return fmt.Errorf("read HPROF string payload: %w", err)
	}
	p.strings[id] = strings.ReplaceAll(string(data), "/", ".")
	p.stringBytes = totalStringBytes
	return nil
}

func (p *hprofParser) parseLoadClassRecord(reader *hprofReader) error {
	if _, err := reader.readU4(); err != nil {
		return err
	}
	classID, err := reader.readID(p.idSize)
	if err != nil {
		return err
	}
	if _, err := reader.readU4(); err != nil {
		return err
	}
	nameID, err := reader.readID(p.idSize)
	if err != nil {
		return err
	}
	if name := p.strings[nameID]; name != "" {
		if _, exists := p.classNames[classID]; !exists && len(p.classNames) >= p.limits.classes {
			p.degradeClassLimit()
			return nil
		}
		p.classNames[classID] = name
	}
	return nil
}
