package main

import (
	"reflect"
	"testing"
)

func TestPersistResultCoversAllSliceFields(t *testing.T) {
	handled := map[string]bool{
		"WholeArchiveRefs": true, "WholeArchivePaths": true, "WholeArchiveCmdPaths": true,
		"AddInclGlobal": true, "OwnAddInclGlobal": true, "ProtoInclude": true,
		"AddInclOneLevel": true, "AddInclUserGlobal": true,
		"CFlagsGlobal": true, "CXXFlagsGlobal": true, "COnlyFlagsGlobal": true,
		"ObjAddLibsGlobal": true, "LDFlagsGlobal": true, "RPathFlagsGlobal": true,
		"PeerArchiveClosureRefs": true, "PeerArchiveClosurePaths": true,
		"PeerGlobalClosureRefs": true, "PeerGlobalClosurePaths": true,
		"PeerWholeArchiveClosureRefs": true, "PeerWholeArchiveClosurePaths": true,
		"PeerWholeArchiveCmdClosurePaths": true,
		"LDPluginRefs":                    true, "LDPluginPaths": true,
		"PeerDynamicClosureRefs": true, "PeerDynamicClosurePaths": true,
		"PeerSbomClosureRefs": true, "PeerSbomClosurePaths": true,
		"InducedDeps": true, "DescClosure": true, "ResourceGlobalClosure": true,
		"GoSrcClosure": true,
	}

	rt := reflect.TypeOf(ModuleEmitResult{})

	for i := 0; i < rt.NumField(); i++ {
		f := rt.Field(i)

		switch f.Type.Kind() {
		case reflect.Slice, reflect.Map, reflect.Array:
			if f.Type.Kind() == reflect.Array && f.Type.Elem().Kind() != reflect.Slice {
				continue
			}

			if !handled[f.Name] {
				t.Errorf("ModuleEmitResult.%s holds references but persistResult does not clone it — module frame pooling would corrupt retained results", f.Name)
			}
		}
	}
}
