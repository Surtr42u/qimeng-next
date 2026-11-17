package media.qimeng.next.ui

import android.graphics.RenderEffect
import android.graphics.RuntimeShader
import android.graphics.Shader
import android.os.Build
import androidx.annotation.RequiresApi
import androidx.compose.runtime.*
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.drawWithContent
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.graphics.asComposeRenderEffect
import androidx.compose.ui.graphics.layer.GraphicsLayer
import androidx.compose.ui.graphics.layer.drawLayer
import androidx.compose.ui.graphics.rememberGraphicsLayer
import androidx.compose.ui.graphics.drawscope.translate
import androidx.compose.ui.layout.onGloballyPositioned
import androidx.compose.ui.layout.positionInRoot
import androidx.compose.ui.platform.LocalView

@Stable
class GlassBackdrop internal constructor(internal val layer: GraphicsLayer) {
    internal var position: Offset = Offset.Zero
    internal var recorded by mutableStateOf(false)
}

@Composable
fun rememberGlassBackdrop(): GlassBackdrop {
    val layer = rememberGraphicsLayer()
    return remember(layer) { GlassBackdrop(layer) }
}

/** Attach only to the scene below the dock; never record the dock into itself. */
fun Modifier.captureGlassBackdrop(backdrop: GlassBackdrop): Modifier =
    onGloballyPositioned { backdrop.position = it.positionInRoot() }.drawWithContent {
        backdrop.layer.record { this@drawWithContent.drawContent() }
        backdrop.recorded = true
        drawLayer(backdrop.layer)
    }

enum class GlassCapability(val description: String) {
    REFRACTED("实时背景采样与折射"), BLURRED("实时背景模糊（无折射）"), SOLID("高对比实色材质")
}

fun glassCapability(apiLevel: Int, hardware: Boolean, enabled: Boolean): GlassCapability = when {
    !enabled || !hardware -> GlassCapability.SOLID
    apiLevel >= 33 -> GlassCapability.REFRACTED
    apiLevel >= 31 -> GlassCapability.BLURRED
    else -> GlassCapability.SOLID
}

@Composable
internal fun rememberBackdropModifier(backdrop: GlassBackdrop, selectedFraction: () -> Float, count: Int, enabled: Boolean): Modifier {
    val capability = glassCapability(Build.VERSION.SDK_INT, LocalView.current.isHardwareAccelerated, enabled)
    val crop = rememberGraphicsLayer()
    var location by remember { mutableStateOf(Offset.Zero) }
    val effect = remember(capability) { if (capability == GlassCapability.REFRACTED) RefractionEffect() else null }
    return Modifier.onGloballyPositioned { location = it.positionInRoot() }.drawWithContent {
        if (backdrop.recorded && capability != GlassCapability.SOLID) {
            crop.record {
                translate(backdrop.position.x - location.x, backdrop.position.y - location.y) { drawLayer(backdrop.layer) }
            }
            if (Build.VERSION.SDK_INT >= 33 && effect != null) {
                crop.renderEffect = effect.update(size.width, size.height, selectedFraction(), count).asComposeRenderEffect()
            } else if (Build.VERSION.SDK_INT >= 31) {
                crop.renderEffect = RenderEffect.createBlurEffect(18f, 18f, Shader.TileMode.CLAMP).asComposeRenderEffect()
            }
            drawLayer(crop)
        }
        drawContent()
    }
}

/** Displaces pixels sampled from the recorded scene, not a gradient pretending to be glass. */
@RequiresApi(33)
private class RefractionEffect {
    private val shader = RuntimeShader("""
        uniform shader scene;
        uniform float2 resolution;
        uniform float selected;
        uniform float count;
        half4 main(float2 p) {
            float h = resolution.y;
            float w = resolution.x / count;
            float2 c = float2((selected + 0.5) * w, h * 0.5);
            float2 d = p - c;
            float2 q = d - float2(clamp(d.x, -max(w * 0.5 - h * 0.42, 0.0), max(w * 0.5 - h * 0.42, 0.0)), 0.0);
            float r = h * 0.42;
            float len = max(length(q), 0.001);
            float edge = smoothstep(r * 0.45, r, len) * (1.0 - smoothstep(r, r + 2.0, len));
            float2 normal = q / len;
            float2 displaced = p - normal * edge * min(h * 0.13, 12.0);
            displaced = clamp(displaced, float2(1.0), resolution - float2(1.0));
            half4 color = scene.eval(displaced);
            float rim = edge * 0.08 * (0.5 - normal.y * 0.5);
            return half4(min(color.rgb + half3(rim), half3(1.0)), color.a);
        }
    """.trimIndent())
    private val renderEffect = RenderEffect.createChainEffect(
        RenderEffect.createRuntimeShaderEffect(shader, "scene"),
        RenderEffect.createBlurEffect(5f, 5f, Shader.TileMode.CLAMP),
    )

    fun update(width: Float, height: Float, selection: Float, count: Int): RenderEffect {
        shader.setFloatUniform("resolution", width, height)
        shader.setFloatUniform("selected", selection)
        shader.setFloatUniform("count", count.toFloat())
        return renderEffect
    }
}
