package main

import (
	"path"
	"strings"
)

var rosCppDeps = []string{
	"sdg/sdc/ros/misc/message_serialization_dictionary",
	"sdg/sdc/bros/common",
	"sdg/sdc/bros/serialization/yamakes/libroscpp_serialization",
	"sdg/sdc/bros/traits/yamakes/roscpp_traits",
}

var rosCppIncludes = []string{
	"sdg/sdc/core/rtb/inplace_vector.h",
	"sdg/sdc/core/rtb/optional.h",
	"sdg/sdc/core/rtb/optional_io.h",
	"sdg/sdc/ros/misc/message_serialization_dictionary/registrator.h",
	"sdg/sdc/bros/serialization/include/ros/serialization.h",
	"sdg/sdc/bros/serialization/include/ros/serialization_streams.h",
	"sdg/sdc/bros/serialization/include/ros/serializer.h",
	"sdg/sdc/bros/traits/include/ros/builtin_message_traits.h",
	"sdg/sdc/bros/traits/include/ros/message_operations.h",
	"sdg/sdc/bros/time/include/ros/duration.h",
	"sdg/sdc/bros/time/include/ros/time.h",
}

var rosZeroCopyIncludes = []string{
	"sdg/sdc/core/rtb/inplace_str.h",
	"sdg/sdc/ros/proto/rosmsg/common_msgs/Duration_ros_converter.h",
	"sdg/sdc/ros/proto/rosmsg/common_msgs/Duration_zero_copy.h",
	"sdg/sdc/ros/proto/rosmsg/common_msgs/Time_ros_converter.h",
	"sdg/sdc/ros/proto/rosmsg/common_msgs/Time_zero_copy.h",
}

const rosGenerator = "sdg/sdc/ros/misc/ros_msg_generator/yamakes/generator"

type RosModule struct {
	Package  string
	Dir      string
	Partial  string
	Language Language
}

func rosModuleUnit(language Language) ModuleUnit {
	switch language {
	case LangRosBindings:
		return ModuleUnit{Type: tokLibrary, Tag: strPy3CppRos, CCTag: strPy3CppRos2, ARPrefix: "libpy3_cpp_", GlobalARTag: strPy3CppRosGlobal, SbomLang: unitSbomCpp}
	case LangPy:
		return ModuleUnit{Type: tokPy3Library, Tag: strPy3Ros, CCTag: strPy3Ros2, ARPrefix: "libpy3", GlobalARTag: strPy3RosGlobal, SbomLang: unitSbomCpp}
	case LangGo:
		return resolveModuleUnitBare(tokGoLibrary, KindLib, language)
	}

	return ModuleUnit{Type: tokLibrary, Tag: strCppRos, CCTag: strCppRos2, ARPrefix: "lib", GlobalARTag: strCppRosGlobal, SbomLang: unitSbomCpp}
}

func setupRosModule(stmt *ModuleStmt, module string, language Language, env Environment, d *ModuleData) {
	if stmt.Name != tokRosLibrary {
		return
	}

	pkg := realPrjName(module)

	if len(stmt.Args) > 0 {
		pkg = stmt.Args[0].string()
	}

	d.multimodule = stmt.Name
	d.ros = &RosModule{Package: pkg, Dir: module, Language: language}
	d.moduleVariants = 1<<LangCPP | 1<<LangRosBindings | 1<<LangPy | 1<<LangGo

	switch language {
	case LangGo:
		env.setBool(envGoRos, true)
		env.setString(envMODULE_TAG, "GO")
	case LangPy:
		d.selfPeers = append(d.selfPeers, LangRosBindings)
		d.peerdirs = append(d.peerdirs, strSdgSdcThirdPartyRosGenpyYamakesPylib.any())
		d.usePython3 = true
		env.setBool(envPy3Ros, true)
	case LangRosBindings:
		d.selfPeers = append(d.selfPeers, LangCPP)
		applyUnknownStmt(nil, module, UnknownStmt{Name: tokUsePython3}, d, env, nil)

		d.peerdirs = append(d.peerdirs, internAnys([]string{
			"contrib/python/numpy/py3",
			"sdg/sdc/ros/misc/ros_msg_generator/yamakes/py_cpp_bindings_support_lib",
			"sdg/sdc/ros/misc/python_utils/yamakes/ros_converters",
		})...)

		env.setBool(envPy3CppRos, true)
	default:
		d.rosInclude = append(d.rosInclude, source(pkg, ":", module))
		d.peerdirs = append(d.peerdirs, internAnys(rosCppDeps)...)
		d.cFlags = append(d.cFlags, strDsdcAlwayslinkOptionIsSet.any())
		env.setBool(envCppRos, true)
	}

	if language != LangGo {
		env.setStringID(envMODULE_TAG, d.unit.Tag)
	}
}

func applyRosStmt(module string, stmt UnknownStmt, env Environment, d *ModuleData) {
	if d.ros == nil {
		return
	}

	args := anyStrs(stmt.Args)

	switch stmt.Name {
	case tokRosDir:
		if len(args) != 1 {
			throwFmt("ROS_DIR expects one directory")
		}

		d.ros.Dir = path.Clean(args[0])
		d.addInclGlobal = append(d.addInclGlobal, build(d.ros.Dir), build(path.Dir(d.ros.Dir)))

		for _, suffix := range []string{"", "/msg", "/srv"} {
			d.rosInclude = append(d.rosInclude, source(d.ros.Package, ":", d.ros.Dir, suffix))
		}
	case tokRosPartial:
		if len(args) != 1 {
			throwFmt("ROS_PARTIAL expects one name")
		}

		d.ros.Partial = args[0]
	default:
		var msgs, srvs, partials []string

		zero := stmt.Name == tokRosZeroCopyMsgCmd

		for i := 0; i < len(args); i++ {
			file := args[i]

			if file == "ZERO_COPY" {
				zero = true

				continue
			}

			if file == "PARTIAL" {
				i++

				if i >= len(args) {
					throwFmt("Expected library name after PARTIAL")
				}

				partials = append(partials, args[i])

				continue
			}

			if !strings.HasSuffix(file, ".msg") && !strings.HasSuffix(file, ".srv") {
				throwFmt("unsupported ROS file: %s", file)
			}

			if zero && strings.HasSuffix(file, ".srv") {
				throwFmt("ZERO_COPY for .srv file is not allowed")
			}

			isMsg := strings.HasSuffix(file, ".msg")
			name := strings.TrimSuffix(path.Base(file), path.Ext(file))

			if isMsg {
				msgs = append(msgs, name)
			} else {
				srvs = append(srvs, name)
			}

			if d.ros.Language == LangPy || (isMsg || d.ros.Language != LangRosBindings) && (isMsg || d.ros.Language != LangGo) {
				d.srcs = append(d.srcs, SrcMeta{Source: internStr(file).any(), Prio: stmtPrioDefault, Seq: d.nextDeclSeq(), RosZeroCopy: zero})
			}

			zero = false
		}

		if stmt.Name == tokRosSrcs {
			finishRosSources(module, d, env, msgs, srvs, partials)
		}
	}
}

func resolveRosInclude(fs FS, includes []VFS, name string) VFS {
	pkg, msg, qualified := strings.Cut(name, "/")

	if !qualified {
		msg = pkg

		if len(includes) == 0 {
			return 0
		}

		pkg, _, _ = strings.Cut(includes[0].relString(), ":")
	}

	for _, inc := range includes {
		p, dir, _ := strings.Cut(inc.relString(), ":")

		if p != pkg {
			continue
		}

		candidate := path.Join(dir, msg+".msg")

		if fs.isFilePathClean(candidate) {
			return source(candidate)
		}
	}

	return 0
}

func finishRosSources(module string, d *ModuleData, env Environment, msgs, srvs, partials []string) {
	namespace := d.ros.Partial

	if namespace == "" {
		namespace = d.ros.Package
	}

	if d.ros.Language == LangPy {
		var pys []ANY

		for _, kind := range []string{"msg", "srv"} {
			out := internStr(kind + "/__init__.py").any()
			args := []string{}
			names := srvs

			if kind == "msg" {
				names = msgs

				if !env.bool(envRosPurePy) {
					args = append(args, "--from-import=..cpp_msg")
					names = nil
				}
			}

			for _, name := range names {
				args = append(args, "--from-import=._"+name)
			}

			for _, partial := range partials {
				args = append(args, "--from-import="+partial+"."+kind)
			}

			d.runPrograms = append(d.runPrograms, &RunProgramStmt{ToolPath: strSdgTier0ToolsGenerateProxyImportFile.any(), Args: internAnys(args), StdoutFile: ptr(out), StdoutNoAuto: true})
			pys = append(pys, out)

			for _, name := range names {
				pys = append(pys, internStr(kind+"/_"+name+".py").any())
			}
		}

		ns := internStr(namespace).any()

		d.pyNamespace = &ns
		d.pySrcs = append(d.pySrcs, pys...)

		scopes := make([]PyScope, len(pys))

		for i := range scopes {
			scopes[i] = PyScope{Namespace: &ns}
		}

		d.pySrcGroups = append(d.pySrcGroups, PySrcGroup{Srcs: pys, Scopes: scopes})
	}

	if d.ros.Language == LangRosBindings {
		out := internStr(namespace + "_py_cpp_bindings.cpp").any()
		args := []string{"--pkg-name=" + d.ros.Package, "--partial-name=" + namespace}
		includes := []string{}

		for _, msg := range msgs {
			args = append(args, "--includes="+msg+".py.h")
			includes = append(includes, msg+".py.h")
		}

		for _, msg := range msgs {
			args = append(args, "--msg-names="+msg)
		}

		includes = append(includes, "sdg/sdc/ros/misc/python_utils/ros_converters.h", "sdg/sdc/ros/misc/python_utils/stl_converters.h", "sdg/sdc/ros/misc/ros_msg_generator/py_cpp/bindings_support_lib/helpers.h")
		d.runPrograms = append(d.runPrograms, &RunProgramStmt{ToolPath: strSdgTier0ToolsGenerateExportPyCppBindings.any(), Args: internAnys(args), StdoutFile: ptr(out), OutputIncludes: internAnys(includes)})
		appendPyRegister(d, namespace+".cpp_msg", true)
	}
}
