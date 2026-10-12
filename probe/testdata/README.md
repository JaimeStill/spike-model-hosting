# probe testdata

Media the router probe sends to a model. Both are copied unchanged from spike-harness-driver,
`clutch/examples/media/` at commit `9981a3e37466e6200c13b3dfcf0d2508cc164fbc`, whose README says
how each was made.

- `shapes.png`: a red circle, a blue square, and a green triangle on white, 480 pixels square.
  The probe asks Gemma 4 26B-A4B to describe it.
- `phrase.wav`: one speaker saying "The access code is seven four two nine.", 16 kHz mono, a few
  seconds long. The probe asks gemma-4-E4B to transcribe it. It stays short because Gemma 4 E4B
  loops on clips longer than about 30 seconds.
