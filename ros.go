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
	d.rosInclude = append(d.rosInclude, source(pkg, ":", module))
	d.peerdirs = append(d.peerdirs, internAnys(rosCppDeps)...)
	d.cFlags = append(d.cFlags, strDsdcAlwayslinkOptionIsSet.any())
	env.setString(envMODULE_TAG, "CPP_ROS")
	env.setBool(envCppRos, true)
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
		zero := stmt.Name == tokRosZeroCopyMsgCmd

		for i := 0; i < len(args); i++ {
			file := args[i]

			if file == "ZERO_COPY" {
				zero = true

				continue
			}

			if file == "PARTIAL" {
				i++

				continue
			}

			if !strings.HasSuffix(file, ".msg") && !strings.HasSuffix(file, ".srv") {
				throwFmt("unsupported ROS file: %s", file)
			}

			if zero && strings.HasSuffix(file, ".srv") {
				throwFmt("ZERO_COPY for .srv file is not allowed")
			}

			d.srcs = append(d.srcs, SrcMeta{Source: internStr(file).any(), Prio: stmtPrioSrcs, Seq: d.nextDeclSeq(), RosZeroCopy: zero})
			zero = false
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
