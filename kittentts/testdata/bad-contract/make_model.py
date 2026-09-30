"""Builds model.onnx: the KittenTTS I/O names, but `duration` is float32
instead of int64, so loading it must fail the model contract check.

uv run --no-project --with onnx python3 kittentts/testdata/bad-contract/make_model.py
"""
import pathlib
import onnx
from onnx import TensorProto, helper

graph = helper.make_graph(
    [
        helper.make_node("Flatten", ["style"], ["flat"], axis=0),
        helper.make_node("Squeeze", ["flat"], ["waveform"]),
        helper.make_node("Cast", ["input_ids"], ["ids_f"], to=TensorProto.FLOAT),
        helper.make_node("Flatten", ["ids_f"], ["ids_flat"], axis=0),
        helper.make_node("Squeeze", ["ids_flat"], ["duration"]),
    ],
    "bad_contract",
    [
        helper.make_tensor_value_info("input_ids", TensorProto.INT64, [1, "n"]),
        helper.make_tensor_value_info("style", TensorProto.FLOAT, [1, 256]),
        helper.make_tensor_value_info("speed", TensorProto.FLOAT, [1]),
    ],
    [
        helper.make_tensor_value_info("waveform", TensorProto.FLOAT, ["samples"]),
        helper.make_tensor_value_info("duration", TensorProto.FLOAT, ["n"]),
    ],
)
model = helper.make_model(graph, opset_imports=[helper.make_opsetid("", 20)], ir_version=9)
onnx.checker.check_model(model)
onnx.save(model, pathlib.Path(__file__).with_name("model.onnx"))
