package aep

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"strconv"

	"github.com/rioam2/rifx"
)

//const MaxUint24 = 1<<24 - 1

type uint24 [3]byte

func (u *uint24) Set(val uint32) {
    if val > (1<<24 - 1) {
        panic("val > 16777215")
    }
    (*u)[0] = uint8((val>>16) & 0xFF)
    (*u)[1] = uint8((val>>8) & 0xFF)
    (*u)[2] = uint8(val & 0xFF)
}

func (u uint24) ToUint32() uint32 {
    return uint32(u[0])<<16 | uint32(u[1])<<8 | uint32(u[2])
}

func (u uint24) ToString() string {
    return strconv.Itoa(int(u.ToUint32()))
}

// ItemTypeName denotes the type of item. See: http://docs.aenhancers.com/items/item/#item-ItemType
type ItemTypeName string

const (
    // ItemTypeFolder denotes a Folder item which may contain additional items
    ItemTypeFolder ItemTypeName = "Folder"
    // ItemTypeComposition denotes a Composition item which has a dimension, length, framerate and child layers
    ItemTypeComposition ItemTypeName = "Composition"
    // ItemTypeFootage denotes an AVItem that has a source (eg: an image or video file)
    ItemTypeFootage ItemTypeName = "Footage"
)

// FootageTypeName denotes the type of footage of an AVItem (eg: Solid, Placeholder, ...)
type FootageTypeName uint16

const (
    // FootageTypeSolid denotes a Solid source
    FootageTypeSolid FootageTypeName = 0x09
    // FootageTypePlaceholder denotes a Placeholder source
    FootageTypePlaceholder FootageTypeName = 0x02
)

// Item is a generalized object storing information about folders, compositions, or footage
type Item struct {
    Name              string
    ID                uint32
    ItemType          ItemTypeName
    FolderContents    []*Item
    FootageDimensions [2]uint16
    FootageFramerate  float64
    FootageSeconds    float64
    Frames              [2]float64        // Will contain start and end frame
    FootageType       FootageTypeName
    BackgroundColor   [3]byte
    CompositionLayers []*Layer
}

func parseItem(itemHead *rifx.List, project *Project) (*Item, error) {
    item := &Item{}
    isRoot := itemHead.Identifier == "Fold"

    // Parse item metadata
    if isRoot {
        item.ID = 0
        item.Name = "root"
        item.ItemType = ItemTypeFolder
    } else {
        nameBlock, err := itemHead.FindByType("Utf8")
        if err != nil {
            return nil, err
        }
        item.Name = nameBlock.ToString()
        type IDTA struct {
            Type      uint16
            Unknown00 [14]byte
            ID        uint32
        }
        itemDescriptor := &IDTA{}
        idtaBlock, err := itemHead.FindByType("idta")
        if err != nil {
            return nil, err
        }
        err = idtaBlock.ToStruct(itemDescriptor)
        if err != nil {
            return nil, err
        }
        item.ID = itemDescriptor.ID
        switch itemDescriptor.Type {
        case 0x01:
            item.ItemType = ItemTypeFolder
        case 0x04:
            item.ItemType = ItemTypeComposition
        case 0x07:
            item.ItemType = ItemTypeFootage
        }
    }

    // Parse unique item type information
    switch item.ItemType {
    case ItemTypeFolder:
        childItemLists := append(itemHead.SublistFilter("Item"), itemHead.SublistMerge("Sfdr").SublistFilter("Item")...)
        for _, childItemList := range childItemLists {
            childItem, err := parseItem(childItemList, project)
            if err != nil {
                return nil, err
            }
            item.FolderContents = append(item.FolderContents, childItem)
        }
    case ItemTypeFootage:
        pinList, err := itemHead.SublistFind("Pin ")
        if err != nil {
            return nil, err
        }
        sspcBlock, err := pinList.FindByType("sspc")
        if err != nil {
            return nil, err
        }
        type SSPC struct {
            Unknown00         [30]byte // Offset 0B
            Width             uint32   // Offset 30B
            Height            uint32   // Offset 34B
            SecondsDividend   uint32   // Offset 38B
            SecondsDivisor    uint32   // Offset 42B
            Unknown01         [10]byte // Offset 46B
            Framerate         uint32   // Offset 56B
            FramerateDividend uint16   // Offset 60B
        }
        sspcDesc := &SSPC{}
        sspcBlock.ToStruct(sspcDesc)
        item.FootageDimensions = [2]uint16{uint16(sspcDesc.Width), uint16(sspcDesc.Height)}
        item.FootageFramerate = float64(sspcDesc.Framerate) + (float64(sspcDesc.FramerateDividend) / float64(1<<16))
        item.FootageSeconds = float64(sspcDesc.SecondsDividend) / float64(sspcDesc.SecondsDivisor)

        optiBlock, err := pinList.FindByType("opti")
        if err != nil {
            return nil, err
        }
        optiData := optiBlock.Data.([]byte)
        item.FootageType = FootageTypeName(binary.BigEndian.Uint16(optiData[4:6]))
        switch item.FootageType {
        case FootageTypeSolid:
            item.Name = fmt.Sprintf("%s", bytes.ReplaceAll(bytes.Trim(optiData[26:255], "\x00"), []byte{0}, []byte{32}))
        case FootageTypePlaceholder:
            item.Name = fmt.Sprintf("%s", bytes.ReplaceAll(bytes.Trim(optiData[10:], "\x00"), []byte{0}, []byte{32}))
        }
    case ItemTypeComposition:
        type CDTA struct {
            Unknown00               [10]byte    // Offset 0B    // Start parsing, skip 10 bytes
            // CDTA OFFSET 10 BYTES
            // 63 64 74 61 00 00 00 CC |
            
            /*FramerateDivisor      uint32      // Offset 4B    
            FramerateDividend       uint32      // Offset 8B
            Unknown01               [32]byte    // Offset 12B
            SecondsDividend         uint32      // Offset 40B
            SecondsDivisor          uint32      // Offset 44B*/    

            //  AE contains frame data in a [3]byte
            //  but we don't have uint24 type by default in go
            //  so we implemented one at the top, 
            //  because using uint32 can seriously fuck us up
            
            /// The hell is this actually? 
            /// Seems there is an empty frame struct in the beginning of the comp    
            Offset1_0               uint16        // Offset         //  [78 00] 00 00 00 00 00 00
            Unknown01               uint24        // Offset         //  78 00 [00 00 00] 00 00 00
            Offset1_1               [3]byte       // Offset         //  78 00 00 00 00 [00 00 00]
            
            Offset2_0               uint16        // Offset         //  [02 58] 00 09 60 00 00 00
            PlayheadPosition        uint24        // Offset         //  02 58 [00 09 60] 00 00 00
            Offset2_1               [3]byte       // Offset         //  02 58 00 00 00 [00 00 00]

            Offset3_0               uint16        // Offset         //  [78 00] 00 01 90 00 00 00
            StartFrame              uint24        // Offset         //  78 00 [00 01 90] 00 00 00
            Offset3_1               [3]byte       // Offset         //  78 00 00 01 90 [00 00 00]

            Offset4_0               uint16        // Offset         //  [78 00] 00 12 C0 00 00 00
            EndFrame                uint24        // Offset         //  78 00 [00 12 C0] 00 00 00]
            Offset4_1               [3]byte       // Offset         //  78 00 00 12 C0 [00 00 00]

            Offset5_0               uint16        // Offset         //  [78 00] 00 1C 20 00 00 00
            CompDuration            uint24        // Offset         //  78 00 [00 1C 20] 00 00 00
            Offset5_1               [3]byte       // Offset         //  78 00 00 1C 20 [00 00 00]

            Offset04                uint16        // Offset 46B     //  [78 00] FF FF FF            
            BackgroundColor         [3]byte       // Offset 48B     //  78 00 [FF FF FF]
            Unknown03               [85]byte      // Offset 51B     //  Empty bytes
            Width                   uint16        // Offset 136B    //  [07 80] 04 38
            Height                  uint16        // Offset 138B    //  07 80 [04 38]
            Unknown04               [12]byte      // Offset 140B    //  Empty bytes
            Framerate               uint16        // Offset 152B    //  [00 3C]
            Unknown05               [7]byte       // Offset         //  00 08 34 

            /// This doesn't looks like a typical framedata, but still close enough
            //Offset6_0             uint16         // Offset        //  [00 00] 00 08 34 00 00
            StartOffset             uint24         // Offset        //  00 00 [00 08 34] 00 00
            Offset6_1               uint16         // Offset        //  00 00 00 08 34 [00 00] 

            /// There is two bytes at the end of [StartOffset] that is used for
            /// comparison. If it's == [Framerate] then use [StartOffset] as is
            /// else divide [StartOffset] by two
            ComparisonFramerate     uint16          // Offset       // [00 3C]
        }
        compDesc := &CDTA{}
        cdataBlock, err := itemHead.FindByType("cdta")
        if err != nil {
            return nil, err
        }
        cdataBlock.ToStruct(compDesc)
        item.FootageDimensions = [2]uint16{compDesc.Width, compDesc.Height}
        //item.FootageFramerate = float64(compDesc.FramerateDividend) / float64(compDesc.FramerateDivisor)
        //item.FootageSeconds = float64(compDesc.SecondsDividend) / float64(compDesc.SecondsDivisor)
        item.FootageFramerate = float64(compDesc.Framerate);
        
        if (compDesc.ComparisonFramerate != compDesc.Framerate) {
            compDesc.StartOffset.Set(uint32(compDesc.StartOffset.ToUint32() / 2))
        }

        if ((compDesc.EndFrame[0] > 0x13 && compDesc.EndFrame[1] > 0xC6 && compDesc.EndFrame[2] > 0x80) || 
            compDesc.EndFrame.ToUint32() > 0x0013C680 || compDesc.Offset4_1[0] > 0x00) {    // hardcoded max comp length
            item.Frames = [2]float64{ float64((compDesc.StartOffset.ToUint32() + compDesc.StartFrame.ToUint32()) / 2), 
                float64((compDesc.StartOffset.ToUint32() + compDesc.CompDuration.ToUint32()) / 2) }
        } else {
            item.Frames = [2]float64{ float64((compDesc.StartOffset.ToUint32() + compDesc.StartFrame.ToUint32()) / 2),
                float64((compDesc.StartOffset.ToUint32() + compDesc.EndFrame.ToUint32()) / 2) }
        }
        
        item.FootageSeconds = float64(compDesc.CompDuration.ToUint32() / 2)
        item.BackgroundColor = compDesc.BackgroundColor

        // Parse composition's layers
        //
        // @LilyStilson: 
        // Disabled, because AErender Launcher does not 
        // need to know what layers composition has

        /*for index, layerListHead := range itemHead.SublistFilter("Layr") {
            layer, err := parseLayer(layerListHead, project)
            if err != nil {
                return nil, err
            }
            layer.Index = uint32(index + 1)
            item.CompositionLayers = append(item.CompositionLayers, layer)
        }*/
    }

    // Insert item into project items map
    project.Items[item.ID] = item

    return item, nil
}
